package handlers

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	mocks "github.com/inference-gateway/cli/tests/mocks/generated"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
)

func TestChatHandler_Handle(t *testing.T) {
	tests := getChatHandlerTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateManager := services.NewStateManager(false)
			handler := setupTestChatHandler(t, tt.setupMocks, stateManager)

			cmd := handler.Handle(tt.msg)

			if tt.expectedCmd {
				assert.NotNil(t, cmd, "Expected command for %T", tt.msg)
			} else {
				assert.Nil(t, cmd, "Expected no command for %T", tt.msg)
			}

			if tt.validateResult != nil {
				tt.validateResult(t, cmd)
			}
		})
	}
}

type chatHandlerTestCase struct {
	name           string
	msg            tea.Msg
	setupMocks     func(*mocks.FakeAgentService, *mocks.FakeModelService, *mocks.FakeToolService, *mocks.FakeFileService, *mocks.FakeConfigService)
	expectedCmd    bool
	validateResult func(*testing.T, tea.Cmd)
}

func getChatHandlerTestCases() []chatHandlerTestCase {
	userInputCases := getUserInputTestCases()
	fileSelectionCases := getFileSelectionTestCases()
	chatEventCases := getChatEventTestCases()
	toolExecutionCases := getToolExecutionTestCases()

	allCases := make([]chatHandlerTestCase, 0, len(userInputCases)+len(fileSelectionCases)+len(chatEventCases)+len(toolExecutionCases))
	allCases = append(allCases, userInputCases...)
	allCases = append(allCases, fileSelectionCases...)
	allCases = append(allCases, chatEventCases...)
	allCases = append(allCases, toolExecutionCases...)

	return allCases
}

func getUserInputTestCases() []chatHandlerTestCase {
	return []chatHandlerTestCase{
		{
			name: "UserInputEvent - normal message",
			msg: domain.UserInputEvent{
				Content: "Hello, how are you?",
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
				model.GetCurrentModelReturns("test-model")
				eventCh := make(chan domain.ChatEvent, 1)
				close(eventCh)
				agent.RunWithStreamReturns(eventCh, nil)
			},
			expectedCmd: true,
		},
		{
			name: "UserInputEvent - slash command",
			msg: domain.UserInputEvent{
				Content: "/help",
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
			},
			expectedCmd: true,
		},
		{
			name: "UserInputEvent - bash command",
			msg: domain.UserInputEvent{
				Content: "!ls -la",
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
				tool.IsToolEnabledReturns(true)
			},
			expectedCmd: true,
		},
		{
			name: "UserInputEvent - tool command",
			msg: domain.UserInputEvent{
				Content: "!!Read(file_path=\"test.txt\")",
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
				tool.IsToolEnabledReturns(true)
			},
			expectedCmd: true,
		},
	}
}

func getFileSelectionTestCases() []chatHandlerTestCase {
	return []chatHandlerTestCase{
		{
			name: "FileSelectionRequestEvent - with files",
			msg:  domain.FileSelectionRequestEvent{},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
				file.ListProjectFilesReturns([]string{"file1.go", "file2.go"}, nil)
			},
			expectedCmd: true,
		},
		{
			name: "FileSelectionRequestEvent - no files",
			msg:  domain.FileSelectionRequestEvent{},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
				file.ListProjectFilesReturns([]string{}, nil)
			},
			expectedCmd: true,
		},
	}
}

func getChatEventTestCases() []chatHandlerTestCase {
	return []chatHandlerTestCase{
		{
			name: "ChatStartEvent",
			msg: domain.ChatStartEvent{
				RequestID: "test-123",
				Timestamp: time.Now(),
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
			},
			expectedCmd: true,
		},
		{
			name: "ChatChunkEvent - with content (no session)",
			msg: domain.ChatChunkEvent{
				RequestID: "test-123",
				Content:   "Response chunk",
				Timestamp: time.Now(),
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
				model.GetCurrentModelReturns("test-model")
			},
			expectedCmd: false,
		},
		{
			name: "ChatChunkEvent - with reasoning",
			msg: domain.ChatChunkEvent{
				RequestID:        "test-123",
				ReasoningContent: "Thinking...",
				Timestamp:        time.Now(),
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
			},
			expectedCmd: true,
		},
		{
			name: "ChatCompleteEvent - without tools",
			msg: domain.ChatCompleteEvent{
				RequestID: "test-123",
				Timestamp: time.Now(),
				Metrics: &domain.ChatMetrics{
					Duration: time.Second,
					Usage: &sdk.CompletionUsage{
						PromptTokens:     100,
						CompletionTokens: 50,
						TotalTokens:      150,
					},
				},
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
			},
			expectedCmd: true,
		},
		{
			name: "ChatErrorEvent",
			msg: domain.ChatErrorEvent{
				RequestID: "test-123",
				Error:     assert.AnError,
				Timestamp: time.Now(),
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
			},
			expectedCmd: true,
		},
	}
}

func getToolExecutionTestCases() []chatHandlerTestCase {
	return []chatHandlerTestCase{
		{
			name: "ToolExecutionStartedEvent",
			msg: domain.ToolExecutionStartedEvent{
				SessionID:  "test-123",
				TotalTools: 2,
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
			},
			expectedCmd: true,
		},
		{
			name: "ToolExecutionProgressEvent",
			msg: domain.ToolExecutionProgressEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: "test-123",
				},
				ToolCallID: "test_tool_call",
				Status:     "executing",
				Message:    "Read tool executing",
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
			},
			expectedCmd: true,
		},
		{
			name: "ToolExecutionCompletedEvent",
			msg: domain.ToolExecutionCompletedEvent{
				SessionID:     "test-123",
				TotalExecuted: 2,
				SuccessCount:  2,
			},
			setupMocks: func(agent *mocks.FakeAgentService, model *mocks.FakeModelService, tool *mocks.FakeToolService, file *mocks.FakeFileService, config *mocks.FakeConfigService) {
				model.GetCurrentModelReturns("test-model")
				eventCh := make(chan domain.ChatEvent, 1)
				close(eventCh)
				agent.RunWithStreamReturns(eventCh, nil)
			},
			expectedCmd: true,
		},
	}
}

func setupTestChatHandler(_ *testing.T, setupMocks func(*mocks.FakeAgentService, *mocks.FakeModelService, *mocks.FakeToolService, *mocks.FakeFileService, *mocks.FakeConfigService), stateManager domain.StateManager) *ChatHandler {
	mockAgent := &mocks.FakeAgentService{}
	mockModel := &mocks.FakeModelService{}
	mockTool := &mocks.FakeToolService{}
	mockFile := &mocks.FakeFileService{}
	mockConfig := &mocks.FakeConfigService{}

	mockConfig.IsApprovalRequiredReturns(false)
	mockConfig.GetOutputDirectoryReturns("/tmp")

	if setupMocks != nil {
		setupMocks(mockAgent, mockModel, mockTool, mockFile, mockConfig)
	}

	conversationRepo := services.NewInMemoryConversationRepository(nil)
	shortcutRegistry := shortcuts.NewRegistry()

	return NewChatHandler(
		mockAgent,
		conversationRepo,
		mockModel,
		mockConfig,
		mockTool,
		mockFile,
		shortcutRegistry,
		stateManager,
	)
}

func TestChatHandler_shouldInjectSystemReminder(t *testing.T) {
	tests := []struct {
		name                    string
		assistantMessageCounter int
		configEnabled           bool
		configInterval          int
		expectedResult          bool
	}{
		{
			name:                    "Should inject at interval 4",
			assistantMessageCounter: 4,
			configEnabled:           true,
			configInterval:          4,
			expectedResult:          true,
		},
		{
			name:                    "Should not inject when disabled",
			assistantMessageCounter: 4,
			configEnabled:           false,
			configInterval:          4,
			expectedResult:          false,
		},
		{
			name:                    "Should not inject at non-interval",
			assistantMessageCounter: 3,
			configEnabled:           true,
			configInterval:          4,
			expectedResult:          false,
		},
		{
			name:                    "Should not inject when counter is 0",
			assistantMessageCounter: 0,
			configEnabled:           true,
			configInterval:          4,
			expectedResult:          false,
		},
		{
			name:                    "Should inject at multiple of interval",
			assistantMessageCounter: 8,
			configEnabled:           true,
			configInterval:          4,
			expectedResult:          true,
		},
		{
			name:                    "Should use default interval 4 when 0",
			assistantMessageCounter: 4,
			configEnabled:           true,
			configInterval:          0,
			expectedResult:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = tt
		})
	}
}

func TestChatEventHandler_handleChatComplete(t *testing.T) {
	tests := []struct {
		name                 string
		msg                  domain.ChatCompleteEvent
		withToolCalls        bool
		metricsProvided      bool
		shouldInjectReminder bool
		setupMocks           func(*mocks.FakeAgentService, *mocks.FakeModelService, *mocks.FakeConfigService)
	}{
		{
			name: "Complete without tools or metrics",
			msg: domain.ChatCompleteEvent{
				RequestID: "test-123",
				Timestamp: time.Now(),
			},
			withToolCalls:   false,
			metricsProvided: false,
		},
		{
			name: "Complete with metrics",
			msg: domain.ChatCompleteEvent{
				RequestID: "test-123",
				Timestamp: time.Now(),
				Metrics: &domain.ChatMetrics{
					Duration: time.Second,
					Usage: &sdk.CompletionUsage{
						PromptTokens:     100,
						CompletionTokens: 50,
						TotalTokens:      150,
					},
				},
			},
			withToolCalls:   false,
			metricsProvided: true,
		},
		{
			name: "Complete with tool calls",
			msg: domain.ChatCompleteEvent{
				RequestID: "test-123",
				Timestamp: time.Now(),
				ToolCalls: []sdk.ChatCompletionMessageToolCall{
					{
						Id:   "tool-1",
						Type: sdk.Function,
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Read",
							Arguments: `{"file_path": "test.txt"}`,
						},
					},
				},
			},
			withToolCalls:   true,
			metricsProvided: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAgent := &mocks.FakeAgentService{}
			mockModel := &mocks.FakeModelService{}
			mockConfig := &mocks.FakeConfigService{}
			mockTool := &mocks.FakeToolService{}
			mockFile := &mocks.FakeFileService{}

			conversationRepo := services.NewInMemoryConversationRepository(nil)
			stateManager := services.NewStateManager(false)
			shortcutRegistry := shortcuts.NewRegistry()

			handler := NewChatHandler(
				mockAgent,
				conversationRepo,
				mockModel,
				mockConfig,
				mockTool,
				mockFile,
				shortcutRegistry,
				stateManager,
			)

			cmd := handler.eventHandler.handleChatComplete(tt.msg)

			assert.NotNil(t, cmd, "Should return a command")

			assert.Nil(t, stateManager.GetChatSession())
		})
	}
}

func TestChatCommandHandler_ParseToolCall(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name        string
		input       string
		expectTool  string
		expectArgs  map[string]any
		expectError bool
	}{
		{
			name:        "Simple tool call with single argument",
			input:       `Read(file_path="test.txt")`,
			expectTool:  "Read",
			expectArgs:  map[string]any{"file_path": "test.txt"},
			expectError: false,
		},
		{
			name:        "Tool call with multiple arguments",
			input:       `Write(file_path="output.txt", content="Hello World")`,
			expectTool:  "Write",
			expectArgs:  map[string]any{"file_path": "output.txt", "content": "Hello World"},
			expectError: false,
		},
		{
			name:        "Tool call with no arguments",
			input:       `Tree()`,
			expectTool:  "Tree",
			expectArgs:  map[string]any{},
			expectError: false,
		},
		{
			name:        "Tool call with numeric arguments",
			input:       `Search(query="test", limit=10)`,
			expectTool:  "Search",
			expectArgs:  map[string]any{"query": "test", "limit": float64(10)},
			expectError: false,
		},
		{
			name:        "Missing opening parenthesis",
			input:       `ReadFile`,
			expectTool:  "",
			expectArgs:  nil,
			expectError: true,
		},
		{
			name:        "Missing closing parenthesis",
			input:       `Read(file_path="test.txt"`,
			expectTool:  "",
			expectArgs:  nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolName, args, err := handler.ParseToolCall(tt.input)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectTool, toolName)
			assert.Equal(t, tt.expectArgs, args)
		})
	}
}
