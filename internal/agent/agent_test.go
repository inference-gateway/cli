package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestAgentServiceImpl_GetMetrics(t *testing.T) {
	tests := []struct {
		name            string
		requestID       string
		setupMetrics    bool
		expectedMetrics *domain.ChatMetrics
	}{
		{
			name:         "get_existing_metrics",
			requestID:    "metrics-123",
			setupMetrics: true,
			expectedMetrics: &domain.ChatMetrics{
				Duration: 2 * time.Second,
				Usage: &sdk.CompletionUsage{
					TotalTokens: 100,
				},
			},
		},
		{
			name:            "get_nonexistent_metrics",
			requestID:       "nonexistent",
			setupMetrics:    false,
			expectedMetrics: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{
				metrics: make(map[string]*domain.ChatMetrics),
			}

			if tt.setupMetrics {
				agentService.metrics[tt.requestID] = tt.expectedMetrics
			}

			result := agentService.GetMetrics(tt.requestID)

			if tt.expectedMetrics == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expectedMetrics.Duration, result.Duration)
				assert.Equal(t, tt.expectedMetrics.Usage.TotalTokens, result.Usage.TotalTokens)
			}
		})
	}
}

func TestAgentServiceImpl_CancelRequest(t *testing.T) {
	tests := []struct {
		name         string
		requestID    string
		setupSession bool
	}{
		{
			name:         "cancel_existing_request",
			requestID:    "cancel-123",
			setupSession: true,
		},
		{
			name:         "cancel_nonexistent_request",
			requestID:    "nonexistent",
			setupSession: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{
				activeSessions: make(map[string]*sessionCancel),
			}

			if tt.setupSession {
				_, cancel := context.WithCancel(context.Background())
				agentService.activeSessions[tt.requestID] = &sessionCancel{
					cancelCtx:  cancel,
					cancelChan: make(chan struct{}),
				}
			}

			err := agentService.CancelRequest(tt.requestID)

			assert.NoError(t, err)
		})
	}
}

func TestAgentServiceImpl_ValidateRequest(t *testing.T) {
	tests := []struct {
		name        string
		request     *domain.AgentRequest
		expectError bool
	}{
		{
			name: "valid_request",
			request: &domain.AgentRequest{
				RequestID: "test-123",
				Model:     "openai/gpt-4",
				Messages: []sdk.Message{
					{Role: sdk.User, Content: sdk.NewMessageContent("Hello")},
				},
			},
			expectError: false,
		},
		{
			name: "missing_request_id",
			request: &domain.AgentRequest{
				Model: "openai/gpt-4",
				Messages: []sdk.Message{
					{Role: sdk.User, Content: sdk.NewMessageContent("Hello")},
				},
			},
			expectError: true,
		},
		{
			name: "missing_model",
			request: &domain.AgentRequest{
				RequestID: "test-123",
				Messages: []sdk.Message{
					{Role: sdk.User, Content: sdk.NewMessageContent("Hello")},
				},
			},
			expectError: true,
		},
		{
			name: "empty_messages",
			request: &domain.AgentRequest{
				RequestID: "test-123",
				Model:     "openai/gpt-4",
				Messages:  []sdk.Message{},
			},
			expectError: true,
		},
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{}

			err := agentService.validateRequest(tt.request)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAgentServiceImpl_StreamingDeltaAccumulation(t *testing.T) {
	tests := []struct {
		name                     string
		streamingDeltas          []string
		expectedContent          string
		validateContentIntegrity func(t *testing.T, finalContent string, deltas []string)
	}{
		{
			name: "content_streaming_without_concatenation",
			streamingDeltas: []string{
				"Hello", "!", " I", "'m", " an", " assistant", " that", " can",
				" help", " you", " with", " Google", " Calendar", " tasks",
				".", " I", " have", " access", " to", " a", " Google",
				" Calendar", " agent", " that", " can", " perform",
				" the", " following", " functions", ":\n\n",
			},
			expectedContent: "Hello! I'm an assistant that can help you with Google Calendar tasks. I have access to a Google Calendar agent that can perform the following functions:\n\n",
			validateContentIntegrity: func(t *testing.T, finalContent string, deltas []string) {
				accumulated := ""
				for _, delta := range deltas {
					accumulated += delta
				}
				assert.Equal(t, accumulated, finalContent, "Streaming deltas should accumulate to final content without concatenation artifacts")
				assert.NotContains(t, finalContent, "give you more details:Hello!", "Should not contain concatenation artifacts")
			},
		},
		{
			name: "incremental_list_formatting",
			streamingDeltas: []string{
				"**", "📅", " Calendar", " Management", " Cap", "abilities", ":**\n\n",
				"1", ".", " **", "List", " Calendar", " Events", "**", " -",
				" View", " your", " upcoming", " events", ",", " meetings",
				",", " and", " appointments", "\n",
				"2", ".", " **", "Create", " Calendar", " Event", "**",
			},
			expectedContent: "**📅 Calendar Management Capabilities:**\n\n1. **List Calendar Events** - View your upcoming events, meetings, and appointments\n2. **Create Calendar Event**",
			validateContentIntegrity: func(t *testing.T, finalContent string, deltas []string) {
				accumulated := ""
				for _, delta := range deltas {
					accumulated += delta
				}
				assert.Equal(t, accumulated, finalContent, "List formatting should be preserved through streaming")
				assert.Contains(t, finalContent, "**📅 Calendar Management Capabilities:**", "Should preserve markdown formatting")
				assert.Contains(t, finalContent, "1. **List Calendar Events**", "Should preserve numbered list formatting")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalContent := ""

			for _, delta := range tt.streamingDeltas {
				finalContent += delta
			}

			assert.Equal(t, tt.expectedContent, finalContent)

			if tt.validateContentIntegrity != nil {
				tt.validateContentIntegrity(t, finalContent, tt.streamingDeltas)
			}
		})
	}
}

func TestNewAgentService(t *testing.T) {
	fakeToolService := &domainmocks.FakeToolService{}
	fakeConversationRepo := &domainmocks.FakeConversationRepository{}
	fakeStateManager := &domainmocks.FakeStateManager{}

	cfg := &config.Config{
		Agent: config.AgentConfig{
			MaxTokens: 4096,
			MaxTurns:  10,
		},
	}

	agentService := NewAgent(
		nil,
		fakeToolService,
		cfg,
		fakeConversationRepo,
		nil,
		nil,
		nil,
		fakeStateManager,
		120,
		nil,
		nil,
	)

	assert.NotNil(t, agentService)
	assert.Equal(t, fakeToolService, agentService.toolService)
	assert.Equal(t, cfg, agentService.config)
	assert.Equal(t, fakeConversationRepo, agentService.conversationRepo)
	assert.Equal(t, fakeStateManager, agentService.stateManager)
	assert.Equal(t, 120, agentService.timeoutSeconds)
	assert.Equal(t, 4096, agentService.maxTokens)
	assert.NotNil(t, agentService.activeRequests)
	assert.NotNil(t, agentService.activeSessions)
	assert.NotNil(t, agentService.metrics)
	assert.NotNil(t, agentService.toolCallsMap)
}

func TestAgentServiceImpl_ParseProvider(t *testing.T) {
	tests := []struct {
		name             string
		model            string
		expectedProvider string
		expectedModel    string
		expectError      bool
	}{
		{
			name:             "valid_openai_model",
			model:            "openai/gpt-4",
			expectedProvider: "openai",
			expectedModel:    "gpt-4",
			expectError:      false,
		},
		{
			name:             "valid_anthropic_model",
			model:            "anthropic/claude-4-opus",
			expectedProvider: "anthropic",
			expectedModel:    "claude-4-opus",
			expectError:      false,
		},
		{
			name:             "valid_model_with_version",
			model:            "openai/gpt-4-turbo-2024-04-09",
			expectedProvider: "openai",
			expectedModel:    "gpt-4-turbo-2024-04-09",
			expectError:      false,
		},
		{
			name:             "model_with_multiple_slashes",
			model:            "local/models/llama-3",
			expectedProvider: "local",
			expectedModel:    "models/llama-3",
			expectError:      false,
		},
		{
			name:        "invalid_model_no_slash",
			model:       "gpt-4",
			expectError: true,
		},
		{
			name:        "empty_model",
			model:       "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{}

			provider, model, err := agentService.parseProvider(tt.model)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedProvider, provider)
				assert.Equal(t, tt.expectedModel, model)
			}
		})
	}
}

func TestAgentServiceImpl_ShouldInjectSystemReminder(t *testing.T) {
	tests := []struct {
		name            string
		turns           int
		remindersConfig config.PromptsAgentRemindersConfig
		expectedResult  bool
	}{
		{
			name:  "reminders_disabled",
			turns: 4,
			remindersConfig: config.PromptsAgentRemindersConfig{
				Enabled:  false,
				Interval: 4,
			},
			expectedResult: false,
		},
		{
			name:  "turn_matches_interval",
			turns: 4,
			remindersConfig: config.PromptsAgentRemindersConfig{
				Enabled:  true,
				Interval: 4,
			},
			expectedResult: true,
		},
		{
			name:  "turn_does_not_match_interval",
			turns: 3,
			remindersConfig: config.PromptsAgentRemindersConfig{
				Enabled:  true,
				Interval: 4,
			},
			expectedResult: false,
		},
		{
			name:  "turn_zero",
			turns: 0,
			remindersConfig: config.PromptsAgentRemindersConfig{
				Enabled:  true,
				Interval: 4,
			},
			expectedResult: false,
		},
		{
			name:  "turn_multiple_of_interval",
			turns: 8,
			remindersConfig: config.PromptsAgentRemindersConfig{
				Enabled:  true,
				Interval: 4,
			},
			expectedResult: true,
		},
		{
			name:  "default_interval_when_zero",
			turns: 4,
			remindersConfig: config.PromptsAgentRemindersConfig{
				Enabled:  true,
				Interval: 0,
			},
			expectedResult: true,
		},
		{
			name:  "negative_interval_defaults_to_4",
			turns: 4,
			remindersConfig: config.PromptsAgentRemindersConfig{
				Enabled:  true,
				Interval: -1,
			},
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Prompts: config.PromptsConfig{
					Agent: config.PromptsAgentConfig{
						SystemReminders: tt.remindersConfig,
					},
				},
			}

			agentService := &AgentServiceImpl{
				config: cfg,
			}

			result := agentService.shouldInjectSystemReminder(tt.turns)

			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestAgentServiceImpl_GetSystemReminderMessage(t *testing.T) {
	tests := []struct {
		name              string
		reminderText      string
		expectDefaultText bool
		expectedSubstring string
	}{
		{
			name:              "custom_reminder_text",
			reminderText:      "Custom reminder message",
			expectDefaultText: false,
			expectedSubstring: "Custom reminder message",
		},
		{
			name:              "default_reminder_text_when_empty",
			reminderText:      "",
			expectDefaultText: true,
			expectedSubstring: "todo list is currently empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Prompts: config.PromptsConfig{
					Agent: config.PromptsAgentConfig{
						SystemReminders: config.PromptsAgentRemindersConfig{
							ReminderText: tt.reminderText,
						},
					},
				},
			}

			agentService := &AgentServiceImpl{
				config: cfg,
			}

			message := agentService.getSystemReminderMessage()

			assert.Equal(t, sdk.User, message.Role)
			content, err := message.Content.AsMessageContent0()
			assert.NoError(t, err)
			assert.Contains(t, content, tt.expectedSubstring)
		})
	}
}

func TestAgentServiceImpl_BuildSandboxInfo(t *testing.T) {
	tests := []struct {
		name           string
		sandboxDirs    []string
		protectedPaths []string
		expectedParts  []string
	}{
		{
			name:           "with_sandbox_dirs_and_protected_paths",
			sandboxDirs:    []string{"/home/user/project", "/tmp"},
			protectedPaths: []string{"/etc", "/root"},
			expectedParts: []string{
				"SANDBOX RESTRICTIONS:",
				"/home/user/project",
				"/tmp",
				"/etc",
				"/root",
				"allowed directories",
				"protected paths",
			},
		},
		{
			name:           "only_sandbox_dirs",
			sandboxDirs:    []string{"/home/user/project"},
			protectedPaths: []string{},
			expectedParts: []string{
				"SANDBOX RESTRICTIONS:",
				"/home/user/project",
				"allowed directories",
			},
		},
		{
			name:           "only_protected_paths",
			sandboxDirs:    []string{},
			protectedPaths: []string{"/etc"},
			expectedParts: []string{
				"SANDBOX RESTRICTIONS:",
				"/etc",
				"protected paths",
			},
		},
		{
			name:           "empty_sandbox_and_protected",
			sandboxDirs:    []string{},
			protectedPaths: []string{},
			expectedParts: []string{
				"SANDBOX RESTRICTIONS:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Sandbox: config.SandboxConfig{
						Directories:    tt.sandboxDirs,
						ProtectedPaths: tt.protectedPaths,
					},
				},
			}

			agentService := &AgentServiceImpl{
				config: cfg,
			}

			result := agentService.buildSandboxInfo()

			for _, part := range tt.expectedParts {
				assert.Contains(t, result, part)
			}
		})
	}
}

func TestAgentServiceImpl_ShouldRequireApproval(t *testing.T) {
	tests := []struct {
		name                 string
		toolCall             *sdk.ChatCompletionMessageToolCall
		isChatMode           bool
		agentMode            domain.AgentMode
		isApprovalRequired   bool
		isBashCommandAllowed bool
		expectedResult       bool
	}{
		{
			name: "auto_accept_mode_never_requires_approval",
			toolCall: &sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Write",
					Arguments: `{"file_path": "/test.txt", "content": "test"}`,
				},
			},
			isChatMode:         true,
			agentMode:          domain.AgentModeAutoAccept,
			isApprovalRequired: true,
			expectedResult:     false,
		},
		{
			name: "non_chat_mode_never_requires_approval",
			toolCall: &sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Write",
					Arguments: `{"file_path": "/test.txt", "content": "test"}`,
				},
			},
			isChatMode:         false,
			agentMode:          domain.AgentModeStandard,
			isApprovalRequired: true,
			expectedResult:     false,
		},
		{
			name: "bash_allowed_command_no_approval",
			toolCall: &sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Bash",
					Arguments: `{"command": "ls -la"}`,
				},
			},
			isChatMode:           true,
			agentMode:            domain.AgentModeStandard,
			isBashCommandAllowed: true,
			expectedResult:       false,
		},
		{
			name: "bash_disallowed_command_requires_approval",
			toolCall: &sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Bash",
					Arguments: `{"command": "rm -rf /"}`,
				},
			},
			isChatMode:           true,
			agentMode:            domain.AgentModeStandard,
			isBashCommandAllowed: false,
			expectedResult:       true,
		},
		{
			name: "bash_invalid_json_requires_approval",
			toolCall: &sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Bash",
					Arguments: `invalid json`,
				},
			},
			isChatMode:     true,
			agentMode:      domain.AgentModeStandard,
			expectedResult: true,
		},
		{
			name: "bash_missing_command_requires_approval",
			toolCall: &sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Bash",
					Arguments: `{"not_command": "value"}`,
				},
			},
			isChatMode:     true,
			agentMode:      domain.AgentModeStandard,
			expectedResult: true,
		},
		{
			name: "non_bash_tool_uses_config",
			toolCall: &sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Write",
					Arguments: `{"file_path": "/test.txt", "content": "test"}`,
				},
			},
			isChatMode:         true,
			agentMode:          domain.AgentModeStandard,
			isApprovalRequired: true,
			expectedResult:     true,
		},
		{
			name: "non_bash_tool_no_approval_when_config_false",
			toolCall: &sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Read",
					Arguments: `{"file_path": "/test.txt"}`,
				},
			},
			isChatMode:         true,
			agentMode:          domain.AgentModeStandard,
			isApprovalRequired: false,
			expectedResult:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Safety: config.SafetyConfig{
						RequireApproval: tt.isApprovalRequired,
					},
					Bash: config.BashToolConfig{
						Mode: config.BashModesConfig{
							All: config.BashModeAllowConfig{Allow: func() []string {
								if tt.isBashCommandAllowed {
									return []string{"ls( .*)?"}
								}
								return []string{}
							}()},
						},
					},
				},
			}

			fakeStateManager := &domainmocks.FakeStateManager{}
			fakeStateManager.GetAgentModeReturns(tt.agentMode)

			approvalPolicy := services.NewStandardApprovalPolicy(cfg, fakeStateManager)

			result := approvalPolicy.ShouldRequireApproval(context.Background(), tt.toolCall, tt.isChatMode)

			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestAgentServiceImpl_CreateErrorEntry(t *testing.T) {
	toolCall := sdk.ChatCompletionMessageToolCall{
		ID: "call-123",
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Write",
			Arguments: `{"file_path": "/test.txt", "content": "test"}`,
		},
	}
	testError := errors.New("test error occurred")
	startTime := time.Now().Add(-2 * time.Second)

	agentService := &AgentServiceImpl{}

	entry := agentService.createErrorEntry(toolCall, testError, startTime)

	assert.Equal(t, sdk.Tool, entry.Message.Role)
	assert.NotNil(t, entry.Message.ToolCallID)
	assert.Equal(t, "call-123", *entry.Message.ToolCallID)

	content, err := entry.Message.Content.AsMessageContent0()
	assert.NoError(t, err)
	assert.Contains(t, content, "Tool execution failed")
	assert.Contains(t, content, "Write")
	assert.Contains(t, content, "test error occurred")

	assert.NotNil(t, entry.ToolExecution)
	assert.Equal(t, "Write", entry.ToolExecution.ToolName)
	assert.False(t, entry.ToolExecution.Success)
	assert.Equal(t, "test error occurred", entry.ToolExecution.Error)
	assert.True(t, entry.ToolExecution.Duration >= 2*time.Second)
}

// makeToolCallChunk creates a tool call chunk for tests
func makeToolCallChunk(index int, id, name, args string) sdk.ChatCompletionMessageToolCallChunk {
	chunk := sdk.ChatCompletionMessageToolCallChunk{
		Index:    index,
		Function: &sdk.ChatCompletionMessageToolCallFunction{Name: name, Arguments: args},
	}
	if id != "" {
		chunk.ID = &id
	}
	return chunk
}

func TestAgentServiceImpl_AccumulateToolCalls(t *testing.T) {
	tests := []struct {
		name           string
		deltas         []sdk.ChatCompletionMessageToolCallChunk
		expectedCalls  int
		validateResult func(t *testing.T, result map[string]*sdk.ChatCompletionMessageToolCall)
	}{
		{
			name: "accumulate_single_tool_call",
			deltas: []sdk.ChatCompletionMessageToolCallChunk{
				makeToolCallChunk(0, "call-1", "Read", `{"file_path":`),
				makeToolCallChunk(0, "", "", `"/test.txt"}`),
			},
			expectedCalls: 1,
			validateResult: func(t *testing.T, result map[string]*sdk.ChatCompletionMessageToolCall) {
				assert.Contains(t, result, "0")
				assert.Equal(t, "call-1", result["0"].ID)
				assert.Equal(t, "Read", result["0"].Function.Name)
				assert.Equal(t, `{"file_path":"/test.txt"}`, result["0"].Function.Arguments)
			},
		},
		{
			name: "accumulate_multiple_tool_calls",
			deltas: []sdk.ChatCompletionMessageToolCallChunk{
				makeToolCallChunk(0, "call-1", "Read", `{"file_path": "/a.txt"}`),
				makeToolCallChunk(1, "call-2", "Read", `{"file_path": "/b.txt"}`),
			},
			expectedCalls: 2,
			validateResult: func(t *testing.T, result map[string]*sdk.ChatCompletionMessageToolCall) {
				assert.Contains(t, result, "0")
				assert.Contains(t, result, "1")
				assert.Equal(t, "call-1", result["0"].ID)
				assert.Equal(t, "call-2", result["1"].ID)
			},
		},
		{
			name:           "empty_deltas",
			deltas:         []sdk.ChatCompletionMessageToolCallChunk{},
			expectedCalls:  0,
			validateResult: func(t *testing.T, result map[string]*sdk.ChatCompletionMessageToolCall) {},
		},
		{
			name: "skip_complete_json_arguments",
			deltas: []sdk.ChatCompletionMessageToolCallChunk{
				makeToolCallChunk(0, "call-1", "Read", `{"file_path": "/test.txt"}`),
				makeToolCallChunk(0, "", "", `extra data`),
			},
			expectedCalls: 1,
			validateResult: func(t *testing.T, result map[string]*sdk.ChatCompletionMessageToolCall) {
				assert.Equal(t, `{"file_path": "/test.txt"}`, result["0"].Function.Arguments)
			},
		},
		{
			name: "preserves_google_thought_signature_across_chunks",
			deltas: func() []sdk.ChatCompletionMessageToolCallChunk {
				sig := "sig-abc-123"
				first := makeToolCallChunk(0, "call-1", "A2A_QueryAgent", `{"agent_url":`)
				second := makeToolCallChunk(0, "", "", `"http://localhost:8081"}`)
				second.ExtraContent = &sdk.ToolCallExtraContent{
					Google: &sdk.ToolCallExtraContent_Google{ThoughtSignature: &sig},
				}
				return []sdk.ChatCompletionMessageToolCallChunk{first, second}
			}(),
			expectedCalls: 1,
			validateResult: func(t *testing.T, result map[string]*sdk.ChatCompletionMessageToolCall) {
				tc := result["0"]
				assert.Equal(t, "call-1", tc.ID)
				assert.Equal(t, `{"agent_url":"http://localhost:8081"}`, tc.Function.Arguments)
				if assert.NotNil(t, tc.ExtraContent) && assert.NotNil(t, tc.ExtraContent.Google) {
					assert.NotNil(t, tc.ExtraContent.Google.ThoughtSignature)
					assert.Equal(t, "sig-abc-123", *tc.ExtraContent.Google.ThoughtSignature)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{
				toolCallsMap: make(map[string]*sdk.ChatCompletionMessageToolCall),
			}

			agentService.accumulateToolCalls(tt.deltas)

			assert.Len(t, agentService.toolCallsMap, tt.expectedCalls)
			if tt.validateResult != nil {
				tt.validateResult(t, agentService.toolCallsMap)
			}
		})
	}
}

func TestAgentServiceImpl_GetAccumulatedToolCalls(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: map[string]*sdk.ChatCompletionMessageToolCall{
			"0": {ID: "call-1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}},
			"1": {ID: "call-2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Write"}},
		},
	}

	result := agentService.getAccumulatedToolCalls()

	assert.Len(t, result, 2)
	assert.Equal(t, "call-1", result[0].ID)
	assert.Equal(t, "Read", result[0].Function.Name)
	assert.Equal(t, "call-2", result[1].ID)
	assert.Equal(t, "Write", result[1].Function.Name)

	assert.Empty(t, agentService.toolCallsMap)
}

func TestAgentServiceImpl_ClearToolCallsMap(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: map[string]*sdk.ChatCompletionMessageToolCall{
			"0": {ID: "call-1"},
			"1": {ID: "call-2"},
		},
	}

	agentService.clearToolCallsMap()

	assert.Empty(t, agentService.toolCallsMap)
	assert.NotNil(t, agentService.toolCallsMap)
}

func TestAgentServiceImpl_StoreIterationMetrics(t *testing.T) {
	tests := []struct {
		name       string
		usage      *sdk.CompletionUsage
		expectSave bool
	}{
		{
			name: "stores_metrics_with_usage",
			usage: &sdk.CompletionUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
			expectSave: true,
		},
		{
			name:       "nil_usage_does_not_store",
			usage:      nil,
			expectSave: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeRepo := &domainmocks.FakeConversationRepository{}

			agentService := &AgentServiceImpl{
				conversationRepo: fakeRepo,
				metrics:          make(map[string]*domain.ChatMetrics),
			}

			startTime := time.Now().Add(-1 * time.Second)
			requestID := "test-request-123"

			agentService.storeIterationMetrics(requestID, "test-model", startTime, tt.usage, nil)

			if tt.expectSave {
				assert.NotNil(t, agentService.metrics[requestID])
				assert.Equal(t, tt.usage, agentService.metrics[requestID].Usage)
				assert.True(t, agentService.metrics[requestID].Duration >= 1*time.Second)
				assert.Equal(t, 1, fakeRepo.AddTokenUsageCallCount())
			} else {
				assert.Nil(t, agentService.metrics[requestID])
				assert.Equal(t, 0, fakeRepo.AddTokenUsageCallCount())
			}
		})
	}
}

func TestGetTruncationRecoveryGuidance(t *testing.T) {
	tests := []struct {
		name           string
		toolName       string
		expectedSubstr []string
	}{
		{
			name:     "write_tool_guidance",
			toolName: "Write",
			expectedSubstr: []string{
				"EMPTY or MINIMAL file",
				"Edit tool",
				"small chunks",
			},
		},
		{
			name:     "edit_tool_guidance",
			toolName: "Edit",
			expectedSubstr: []string{
				"SMALLER chunks",
				"10-20 lines",
				"multiple smaller Edit calls",
			},
		},
		{
			name:     "bash_tool_guidance",
			toolName: "Bash",
			expectedSubstr: []string{
				"command output",
				"smaller parts",
			},
		},
		{
			name:     "unknown_tool_guidance",
			toolName: "UnknownTool",
			expectedSubstr: []string{
				"tool arguments were too large",
				"smaller, incremental operations",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTruncationRecoveryGuidance(tt.toolName)

			for _, substr := range tt.expectedSubstr {
				assert.Contains(t, result, substr)
			}
		})
	}
}

func TestEventPublisher_PublishChatStart(t *testing.T) {
	chatEvents := make(chan domain.ChatEvent, 1)
	publisher := newEventPublisher("request-123", chatEvents)

	publisher.publishChatStart()

	select {
	case event := <-chatEvents:
		startEvent, ok := event.(domain.ChatStartEvent)
		assert.True(t, ok)
		assert.Equal(t, "request-123", startEvent.RequestID)
		assert.False(t, startEvent.Timestamp.IsZero())
	default:
		t.Fatal("expected event to be published")
	}
}

func TestEventPublisher_PublishChatComplete(t *testing.T) {
	chatEvents := make(chan domain.ChatEvent, 1)
	publisher := newEventPublisher("request-123", chatEvents)

	toolCalls := []sdk.ChatCompletionMessageToolCall{
		{ID: "call-1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}},
	}
	metrics := &domain.ChatMetrics{
		Duration: 2 * time.Second,
		Usage:    &sdk.CompletionUsage{TotalTokens: 100},
	}

	publisher.publishChatComplete("", toolCalls, metrics)

	select {
	case event := <-chatEvents:
		completeEvent, ok := event.(domain.ChatCompleteEvent)
		assert.True(t, ok)
		assert.Equal(t, "request-123", completeEvent.RequestID)
		assert.Len(t, completeEvent.ToolCalls, 1)
		assert.Equal(t, metrics, completeEvent.Metrics)
	default:
		t.Fatal("expected event to be published")
	}
}

func TestEventPublisher_PublishChatChunk(t *testing.T) {
	chatEvents := make(chan domain.ChatEvent, 1)
	publisher := newEventPublisher("request-123", chatEvents)

	callID := "call-1"
	toolCalls := []sdk.ChatCompletionMessageToolCallChunk{
		{Index: 0, ID: &callID},
	}

	publisher.publishChatChunk("Hello", "thinking...", toolCalls)

	select {
	case event := <-chatEvents:
		chunkEvent, ok := event.(domain.ChatChunkEvent)
		assert.True(t, ok)
		assert.Equal(t, "request-123", chunkEvent.RequestID)
		assert.Equal(t, "Hello", chunkEvent.Content)
		assert.Equal(t, "thinking...", chunkEvent.ReasoningContent)
		assert.True(t, chunkEvent.Delta)
		assert.Len(t, chunkEvent.ToolCalls, 1)
	default:
		t.Fatal("expected event to be published")
	}
}

func TestEventPublisher_PublishOptimizationStatus(t *testing.T) {
	chatEvents := make(chan domain.ChatEvent, 1)
	publisher := newEventPublisher("request-123", chatEvents)

	publisher.publishOptimizationStatus("Optimizing...", true, 10, 5)

	select {
	case event := <-chatEvents:
		optimizationEvent, ok := event.(domain.OptimizationStatusEvent)
		assert.True(t, ok)
		assert.Equal(t, "request-123", optimizationEvent.RequestID)
		assert.Equal(t, "Optimizing...", optimizationEvent.Message)
		assert.True(t, optimizationEvent.IsActive)
		assert.Equal(t, 10, optimizationEvent.OriginalCount)
		assert.Equal(t, 5, optimizationEvent.OptimizedCount)
	default:
		t.Fatal("expected event to be published")
	}
}

func TestEventPublisher_PublishToolsQueued(t *testing.T) {
	chatEvents := make(chan domain.ChatEvent, 10)
	publisher := newEventPublisher("request-123", chatEvents)

	toolCalls := []sdk.ChatCompletionMessageToolCall{
		{ID: "call-1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: "{}"}},
		{ID: "call-2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Write", Arguments: "{}"}},
	}

	publisher.publishToolsQueued(toolCalls)

	for i, tc := range toolCalls {
		select {
		case event := <-chatEvents:
			progressEvent, ok := event.(domain.ToolExecutionProgressEvent)
			assert.True(t, ok, "expected ToolExecutionProgressEvent for tool %d", i)
			assert.Equal(t, "request-123", progressEvent.RequestID)
			assert.Equal(t, tc.ID, progressEvent.ToolCallID)
			assert.Equal(t, tc.Function.Name, progressEvent.ToolName)
			assert.Equal(t, "queued", progressEvent.Status)
		default:
			t.Fatalf("expected event to be published for tool %d", i)
		}
	}
}

func TestEventPublisher_PublishToolStatusChange(t *testing.T) {
	chatEvents := make(chan domain.ChatEvent, 1)
	publisher := newEventPublisher("request-123", chatEvents)

	publisher.publishToolStatusChange("call-1", "TestTool", "running", "Executing...", nil)

	select {
	case event := <-chatEvents:
		progressEvent, ok := event.(domain.ToolExecutionProgressEvent)
		assert.True(t, ok)
		assert.Equal(t, "request-123", progressEvent.RequestID)
		assert.Equal(t, "call-1", progressEvent.ToolCallID)
		assert.Equal(t, "running", progressEvent.Status)
		assert.Equal(t, "Executing...", progressEvent.Message)
	default:
		t.Fatal("expected event to be published")
	}
}

// TestExecuteToolInternal_PublishesTerminalStatus guards the asymmetry that caused the
// per-tool ticker to keep counting on the approval path: executeToolInternal must publish a
// terminal "completed"/"failed" status itself (so the UI freezes the elapsed-time ticker),
// rather than relying on executeToolCallsParallel to publish it externally - the approval path
// calls executeToolInternal directly and bypasses that.
func TestExecuteToolInternal_PublishesTerminalStatus(t *testing.T) {
	tests := []struct {
		name           string
		success        bool
		expectedStatus string
	}{
		{name: "success publishes completed", success: true, expectedStatus: "completed"},
		{name: "failure publishes failed", success: false, expectedStatus: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeToolService := &domainmocks.FakeToolService{}
			fakeToolService.ExecuteToolReturns(&domain.ToolExecutionResult{
				ToolName: "Read",
				Success:  tt.success,
			}, nil)

			fakeRepo := &domainmocks.FakeConversationRepository{}
			fakeRepo.FormatToolResultForLLMReturns("formatted result")

			s := &AgentServiceImpl{
				toolService:      fakeToolService,
				conversationRepo: fakeRepo,
			}

			chatEvents := make(chan domain.ChatEvent, 32)
			publisher := newEventPublisher("request-123", chatEvents)

			tc := sdk.ChatCompletionMessageToolCall{
				ID:       "call-1",
				Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: "{}"},
			}

			entry := s.executeToolInternal(context.Background(), tc, publisher, true, time.Now())
			require.NotNil(t, entry.ToolExecution)
			assert.Equal(t, tt.success, entry.ToolExecution.Success)

			var lastStatus string
		drain:
			for {
				select {
				case event := <-chatEvents:
					if progress, ok := event.(domain.ToolExecutionProgressEvent); ok {
						lastStatus = progress.Status
					}
				default:
					break drain
				}
			}
			assert.Equal(t, tt.expectedStatus, lastStatus, "final tool status event should be terminal so the UI freezes the ticker")
		})
	}
}

func TestAgentServiceImpl_CancelRequest_WithCancelChannel(t *testing.T) {
	agentService := &AgentServiceImpl{
		activeRequests: make(map[string]context.CancelFunc),
		activeSessions: make(map[string]*sessionCancel),
	}

	_, cancel := context.WithCancel(context.Background())
	cancelChan := make(chan struct{})
	agentService.activeSessions["request-123"] = &sessionCancel{
		cancelCtx:  cancel,
		cancelChan: cancelChan,
	}

	err := agentService.CancelRequest("request-123")

	assert.NoError(t, err)

	select {
	case <-cancelChan:
	default:
		t.Fatal("expected cancel signal to be sent")
	}
}

func TestAgentServiceImpl_CancelRequest_WithBothContextAndChannel(t *testing.T) {
	agentService := &AgentServiceImpl{
		activeRequests: make(map[string]context.CancelFunc),
		activeSessions: make(map[string]*sessionCancel),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelChan := make(chan struct{})
	agentService.activeSessions["request-123"] = &sessionCancel{
		cancelCtx:  cancel,
		cancelChan: cancelChan,
	}

	err := agentService.CancelRequest("request-123")

	assert.NoError(t, err)

	assert.Error(t, ctx.Err())

	select {
	case <-cancelChan:
	default:
		t.Fatal("expected cancel signal")
	}
}

// TestAgentServiceImpl_CancelRequest_IsIdempotent verifies that multiple
// Esc presses (or any repeated CancelRequest calls) are safe - no panic on
// double-close of the cancel channel, and every call returns nil.
func TestAgentServiceImpl_CancelRequest_IsIdempotent(t *testing.T) {
	agentService := &AgentServiceImpl{
		activeRequests: make(map[string]context.CancelFunc),
		activeSessions: make(map[string]*sessionCancel),
	}

	_, cancel := context.WithCancel(context.Background())
	cancelChan := make(chan struct{})
	agentService.activeSessions["request-123"] = &sessionCancel{
		cancelCtx:  cancel,
		cancelChan: cancelChan,
	}

	for range 5 {
		assert.NoError(t, agentService.CancelRequest("request-123"))
	}

	select {
	case <-cancelChan:
	default:
		t.Fatal("expected cancel signal after repeated CancelRequest calls")
	}
}

// TestAgentServiceImpl_CancelRequest_CancelsSessionContext verifies that
// cancellation propagates through the session-level context so downstream
// tool execution and approval waits observe ctx.Done().
func TestAgentServiceImpl_CancelRequest_CancelsSessionContext(t *testing.T) {
	agentService := &AgentServiceImpl{
		activeRequests: make(map[string]context.CancelFunc),
		activeSessions: make(map[string]*sessionCancel),
	}

	sessionCtx, cancel := context.WithCancel(context.Background())
	agentService.activeSessions["request-123"] = &sessionCancel{
		cancelCtx:  cancel,
		cancelChan: make(chan struct{}),
	}

	require.NoError(t, agentService.CancelRequest("request-123"))

	assert.Equal(t, context.Canceled, sessionCtx.Err())
}

// TestSessionCancel_OnlyClosesOnce verifies the sync.Once contract: the
// cancel channel is closed at most once, and the cancelCtx func is called
// at most once, even when Cancel is invoked many times concurrently.
func TestSessionCancel_OnlyClosesOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancelCallCount := 0
	wrappedCancel := func() {
		cancelCallCount++
		cancel()
	}

	sc := &sessionCancel{
		cancelCtx:  wrappedCancel,
		cancelChan: make(chan struct{}),
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc.Cancel()
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, cancelCallCount, "cancelCtx should be invoked exactly once")
	assert.Equal(t, context.Canceled, ctx.Err())

	select {
	case _, ok := <-sc.cancelChan:
		assert.False(t, ok, "cancelChan should be closed")
	default:
		t.Fatal("expected cancelChan to be closed")
	}
}

func TestAgentServiceImpl_GetSystemPromptForMode(t *testing.T) {
	tests := []struct {
		name            string
		agentMode       domain.AgentMode
		systemPrompt    string
		planPrompt      string
		expectedPrompt  string
		nilStateManager bool
	}{
		{
			name:            "nil_state_manager_returns_default",
			nilStateManager: true,
			systemPrompt:    "Default prompt",
			planPrompt:      "Plan prompt",
			expectedPrompt:  "Default prompt",
		},
		{
			name:           "standard_mode_returns_default",
			agentMode:      domain.AgentModeStandard,
			systemPrompt:   "Default prompt",
			planPrompt:     "Plan prompt",
			expectedPrompt: "Default prompt",
		},
		{
			name:           "plan_mode_returns_plan_prompt",
			agentMode:      domain.AgentModePlan,
			systemPrompt:   "Default prompt",
			planPrompt:     "Plan prompt",
			expectedPrompt: "Plan prompt",
		},
		{
			name:           "plan_mode_falls_back_to_default_if_plan_empty",
			agentMode:      domain.AgentModePlan,
			systemPrompt:   "Default prompt",
			planPrompt:     "",
			expectedPrompt: "Default prompt",
		},
		{
			name:           "auto_accept_mode_returns_default",
			agentMode:      domain.AgentModeAutoAccept,
			systemPrompt:   "Default prompt",
			planPrompt:     "Plan prompt",
			expectedPrompt: "Default prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Prompts: config.PromptsConfig{
					Agent: config.PromptsAgentConfig{
						SystemPrompt:     tt.systemPrompt,
						SystemPromptPlan: tt.planPrompt,
					},
				},
			}

			agentService := &AgentServiceImpl{
				config: cfg,
			}

			if !tt.nilStateManager {
				fakeStateManager := &domainmocks.FakeStateManager{}
				fakeStateManager.GetAgentModeReturns(tt.agentMode)
				agentService.stateManager = fakeStateManager
			}

			result := agentService.getSystemPromptForMode()

			assert.Equal(t, tt.expectedPrompt, result)
		})
	}
}

func TestAgentServiceImpl_AddSystemPrompt(t *testing.T) {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			SystemPromptWithDefaults: true,
		},
		Prompts: config.PromptsConfig{
			Agent: config.PromptsAgentConfig{
				SystemPrompt: "You are a helpful assistant.",
			},
		},
		Tools: config.ToolsConfig{
			Sandbox: config.SandboxConfig{
				Directories:    []string{"/home/user"},
				ProtectedPaths: []string{"/etc"},
			},
		},
	}

	agentService := &AgentServiceImpl{
		config: cfg,
	}

	inputMessages := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("Hello")},
	}

	result := agentService.addSystemPrompt(inputMessages)

	assert.Len(t, result, 2)
	assert.Equal(t, sdk.System, result[0].Role)

	content, err := result[0].Content.AsMessageContent0()
	assert.NoError(t, err)
	assert.Contains(t, content, "You are a helpful assistant.")
	assert.Contains(t, content, "SANDBOX RESTRICTIONS:")
	assert.Contains(t, content, "/home/user")
	assert.Contains(t, content, "/etc")

	assert.Equal(t, sdk.User, result[1].Role)
}

func TestAgentServiceImpl_BuildSystemPrompt(t *testing.T) {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			SystemPromptWithDefaults: true,
		},
		Prompts: config.PromptsConfig{
			Agent: config.PromptsAgentConfig{
				SystemPrompt: "You are a helpful assistant.",
			},
		},
		Tools: config.ToolsConfig{
			Sandbox: config.SandboxConfig{
				Directories:    []string{"/home/user"},
				ProtectedPaths: []string{"/etc"},
			},
		},
	}

	agentService := &AgentServiceImpl{
		config: cfg,
	}

	prompt := agentService.BuildSystemPrompt()

	assert.Contains(t, prompt, "You are a helpful assistant.")
	assert.Contains(t, prompt, "SANDBOX RESTRICTIONS:")
	assert.Contains(t, prompt, "Current date and time:")

	systemMsg := agentService.addSystemPrompt(nil)[0]
	content, err := systemMsg.Content.AsMessageContent0()
	assert.NoError(t, err)
	const tsMarker = "Current date and time:"
	assert.Equal(t, strings.SplitN(content, tsMarker, 2)[0], strings.SplitN(prompt, tsMarker, 2)[0])
}

func TestAgentServiceImpl_BuildSystemPrompt_EmptyPrompt(t *testing.T) {
	agentService := &AgentServiceImpl{
		config: &config.Config{
			Prompts: config.PromptsConfig{
				Agent: config.PromptsAgentConfig{SystemPrompt: ""},
			},
		},
	}

	assert.Equal(t, "", agentService.BuildSystemPrompt())
}

func TestAgentServiceImpl_AddSystemPrompt_EmptyPrompt(t *testing.T) {
	cfg := &config.Config{
		Prompts: config.PromptsConfig{
			Agent: config.PromptsAgentConfig{
				SystemPrompt: "",
			},
		},
	}

	agentService := &AgentServiceImpl{
		config: cfg,
	}

	inputMessages := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("Hello")},
	}

	result := agentService.addSystemPrompt(inputMessages)

	assert.Len(t, result, 1)
	assert.Equal(t, sdk.User, result[0].Role)
}

func TestAgentServiceImpl_ConcurrentMetricsAccess(t *testing.T) {
	agentService := &AgentServiceImpl{
		metrics: make(map[string]*domain.ChatMetrics),
	}

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			agentService.metricsMux.Lock()
			agentService.metrics[string(rune('a'+id%26))] = &domain.ChatMetrics{
				Duration: time.Duration(id) * time.Second,
			}
			agentService.metricsMux.Unlock()
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = agentService.GetMetrics("a")
		}()
	}

	wg.Wait()
}

func TestAgentServiceImpl_ConcurrentToolCallsAccess(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: make(map[string]*sdk.ChatCompletionMessageToolCall),
	}

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			agentService.accumulateToolCalls([]sdk.ChatCompletionMessageToolCallChunk{
				makeToolCallChunk(id, "call-"+string(rune('a'+id%26)), "Read", `{}`),
			})
		}(i)
	}

	for i := 0; i < numGoroutines/10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agentService.clearToolCallsMap()
		}()
	}

	wg.Wait()
}

func TestAgentServiceImpl_BuildA2AAgentInfo(t *testing.T) {
	createFakeA2AAgentService := func(agents []string) *domainmocks.FakeA2AAgentService {
		fake := &domainmocks.FakeA2AAgentService{}
		fake.GetConfiguredAgentsReturns(agents)
		fake.GetAgentCardsReturns(nil, nil)
		return fake
	}

	tests := []struct {
		name            string
		a2aAgentService domain.A2AAgentService
		expectedParts   []string
		expectedEmpty   bool
	}{
		{
			name:            "nil_a2a_service_returns_empty",
			a2aAgentService: nil,
			expectedEmpty:   true,
		},
		{
			name:            "empty_agents_returns_empty",
			a2aAgentService: createFakeA2AAgentService([]string{}),
			expectedEmpty:   true,
		},
		{
			name:            "with_configured_agents",
			a2aAgentService: createFakeA2AAgentService([]string{"http://agent1.local", "http://agent2.local"}),
			expectedParts: []string{
				"Available A2A Agents:",
				"http://agent1.local",
				"http://agent2.local",
				"A2A_SubmitTask tool",
			},
			expectedEmpty: false,
		},
		{
			name:            "single_agent",
			a2aAgentService: createFakeA2AAgentService([]string{"http://single-agent.local"}),
			expectedParts: []string{
				"Available A2A Agents:",
				"http://single-agent.local",
			},
			expectedEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{
				a2aAgentService: tt.a2aAgentService,
			}

			result := agentService.buildA2AAgentInfo()

			if tt.expectedEmpty {
				assert.Empty(t, result)
			} else {
				for _, part := range tt.expectedParts {
					assert.Contains(t, result, part)
				}
			}
		})
	}
}

// TestAgentServiceImpl_BatchDrainQueue_ClosesOrphanToolCalls is the
// regression test for the bug where a user message queued during tool
// execution + Esc would land in the conversation immediately after an
// assistant message with orphan tool_calls, producing a payload the
// gateway rejected with HTTP 400 ("insufficient tool messages
// following tool_calls message").
func TestAgentServiceImpl_BatchDrainQueue_ClosesOrphanToolCalls(t *testing.T) {
	toolCalls := []sdk.ChatCompletionMessageToolCall{
		{ID: "tc-a", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}},
		{ID: "tc-b", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}},
		{ID: "tc-c", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Tree"}},
	}
	conversation := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("read 3 files")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("reading"), ToolCalls: &toolCalls},
	}

	queue := services.NewMessageQueueService()
	queue.Enqueue(sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Hi")}, "req-1")

	repo := &domainmocks.FakeConversationRepository{}

	svc := &AgentServiceImpl{
		messageQueue:     queue,
		conversationRepo: repo,
	}

	eventCh := make(chan domain.ChatEvent, 16)
	publisher := newEventPublisher("req-1", eventCh)

	drained := svc.batchDrainQueue(&conversation, publisher)

	assert.Equal(t, 1, drained, "exactly one queued message should be drained")
	require.Len(t, conversation, 6, "conversation must be user, assistant, tool×3, user")

	assert.Equal(t, sdk.User, conversation[0].Role)
	assert.Equal(t, sdk.Assistant, conversation[1].Role)
	assert.Equal(t, sdk.Tool, conversation[2].Role)
	assert.Equal(t, sdk.Tool, conversation[3].Role)
	assert.Equal(t, sdk.Tool, conversation[4].Role)
	assert.Equal(t, sdk.User, conversation[5].Role, "queued user message must land AFTER synthetic tool responses")

	require.NotNil(t, conversation[2].ToolCallID)
	require.NotNil(t, conversation[3].ToolCallID)
	require.NotNil(t, conversation[4].ToolCallID)
	assert.Equal(t, "tc-a", *conversation[2].ToolCallID, "synthetics must preserve original tool_calls order")
	assert.Equal(t, "tc-b", *conversation[3].ToolCallID)
	assert.Equal(t, "tc-c", *conversation[4].ToolCallID)

	body, err := conversation[2].Content.AsMessageContent0()
	require.NoError(t, err)
	assert.Equal(t, services.CancelledToolResponseContent, body)

	close(eventCh)
	var cancelled []domain.ToolCancelledEvent
	var queued []domain.MessageQueuedEvent
	for ev := range eventCh {
		switch e := ev.(type) {
		case domain.ToolCancelledEvent:
			cancelled = append(cancelled, e)
		case domain.MessageQueuedEvent:
			queued = append(queued, e)
		}
	}
	require.Len(t, cancelled, 3, "expected 3 ToolCancelledEvents")
	assert.Equal(t, "tc-a", cancelled[0].ToolCallID)
	assert.Equal(t, "Read", cancelled[0].ToolName)
	assert.Equal(t, "tc-c", cancelled[2].ToolCallID)
	assert.Equal(t, "Tree", cancelled[2].ToolName)
	assert.Len(t, queued, 1, "expected 1 MessageQueuedEvent for the drained user message")

	require.Equal(t, 4, repo.AddMessageCallCount(), "repo must record 3 synthetics + 1 queued message")
	assert.Equal(t, sdk.Tool, repo.AddMessageArgsForCall(0).Message.Role, "synthetics must be persisted before the queued user message so JSONL append order matches logical order")
	assert.Equal(t, sdk.Tool, repo.AddMessageArgsForCall(1).Message.Role)
	assert.Equal(t, sdk.Tool, repo.AddMessageArgsForCall(2).Message.Role)
	assert.Equal(t, sdk.User, repo.AddMessageArgsForCall(3).Message.Role)
}

func TestAgentServiceImpl_BatchDrainQueue_IdempotentOnRepairedConversation(t *testing.T) {
	idA := "tc-x"
	toolCalls := []sdk.ChatCompletionMessageToolCall{
		{ID: idA, Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}},
	}
	conversation := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("u1")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent(""), ToolCalls: &toolCalls},
		{Role: sdk.Tool, Content: sdk.NewMessageContent(services.CancelledToolResponseContent), ToolCallID: &idA},
	}

	queue := services.NewMessageQueueService()
	queue.Enqueue(sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("u2")}, "req-x")

	repo := &domainmocks.FakeConversationRepository{}
	svc := &AgentServiceImpl{
		messageQueue:     queue,
		conversationRepo: repo,
	}

	eventCh := make(chan domain.ChatEvent, 8)
	publisher := newEventPublisher("req-x", eventCh)

	drained := svc.batchDrainQueue(&conversation, publisher)
	assert.Equal(t, 1, drained)
	require.Len(t, conversation, 4, "no new synthetics should be inserted")

	close(eventCh)
	var cancelled int
	for ev := range eventCh {
		if _, ok := ev.(domain.ToolCancelledEvent); ok {
			cancelled++
		}
	}
	assert.Equal(t, 0, cancelled, "validator must be a no-op on already-repaired conversation")
}
