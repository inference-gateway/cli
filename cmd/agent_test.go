package cmd

import (
	"errors"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestIsModelAvailable(t *testing.T) {
	models := []string{"openai/gpt-4", "anthropic/claude-4", "openai/gpt-4.5-turbo"}

	tests := []struct {
		name        string
		targetModel string
		expected    bool
	}{
		{
			name:        "Model exists",
			targetModel: "openai/gpt-4",
			expected:    true,
		},
		{
			name:        "Model does not exist",
			targetModel: "google/gemini",
			expected:    false,
		},
		{
			name:        "Empty target model",
			targetModel: "",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isModelAvailable(models, tt.targetModel)
			if result != tt.expected {
				t.Errorf("isModelAvailable() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildSDKMessages(t *testing.T) {
	session := &AgentSession{
		conversation: []ConversationMessage{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: mockTime(),
			},
			{
				Role:      "assistant",
				Content:   "Hi there!",
				Timestamp: mockTime(),
			},
		},
	}

	messages := session.buildSDKMessages()

	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}

	if messages[0].Role != sdk.User {
		t.Errorf("Expected first message role to be 'user', got %v", messages[0].Role)
	}

	if messages[1].Role != sdk.Assistant {
		t.Errorf("Expected second message role to be 'assistant', got %v", messages[1].Role)
	}

	content0, _ := messages[0].Content.AsMessageContent0()
	if content0 != "Hello" {
		t.Errorf("Expected first message content to be 'Hello', got %s", content0)
	}
}

func TestExecuteToolCallsParallel(t *testing.T) {
	tests := []struct {
		name              string
		toolCalls         []sdk.ChatCompletionMessageToolCall
		maxConcurrentTool int
		mockResults       []*domain.ToolExecutionResult
		mockErrors        []error
		expectedCount     int
		expectedRoles     []string
	}{
		{
			name:              "empty tool calls",
			toolCalls:         []sdk.ChatCompletionMessageToolCall{},
			maxConcurrentTool: 5,
			mockResults:       []*domain.ToolExecutionResult{},
			mockErrors:        []error{},
			expectedCount:     0,
			expectedRoles:     []string{},
		},
		{
			name: "single tool call",
			toolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					Id: "call_1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Read",
						Arguments: `{"file_path": "test.txt"}`,
					},
				},
			},
			maxConcurrentTool: 5,
			mockResults: []*domain.ToolExecutionResult{
				{
					ToolName: "Read",
					Success:  true,
					Data:     "file content",
				},
			},
			mockErrors:    []error{nil},
			expectedCount: 1,
			expectedRoles: []string{"tool"},
		},
		{
			name: "multiple tool calls",
			toolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					Id: "call_1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Read",
						Arguments: `{"file_path": "test1.txt"}`,
					},
				},
				{
					Id: "call_2",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Grep",
						Arguments: `{"pattern": "func"}`,
					},
				},
				{
					Id: "call_3",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Write",
						Arguments: `{"file_path": "output.txt", "content": "hello"}`,
					},
				},
			},
			maxConcurrentTool: 2,
			mockResults: []*domain.ToolExecutionResult{
				{ToolName: "Read", Success: true, Data: "content1"},
				{ToolName: "Grep", Success: true, Data: "matches"},
				{ToolName: "Write", Success: true, Data: "written"},
			},
			mockErrors:    []error{nil, nil, nil},
			expectedCount: 3,
			expectedRoles: []string{"tool", "tool", "tool"},
		},
		{
			name: "tool call with error",
			toolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					Id: "call_1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Read",
						Arguments: `{"file_path": "nonexistent.txt"}`,
					},
				},
			},
			maxConcurrentTool: 5,
			mockResults:       []*domain.ToolExecutionResult{nil},
			mockErrors:        []error{errors.New("file not found")},
			expectedCount:     1,
			expectedRoles:     []string{"tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockToolService := &domainmocks.FakeToolService{}

			for i := range tt.toolCalls {
				if i < len(tt.mockErrors) && tt.mockErrors[i] != nil {
					mockToolService.ExecuteToolReturns(tt.mockResults[i], tt.mockErrors[i])
				} else if i < len(tt.mockResults) {
					mockToolService.ExecuteToolReturns(tt.mockResults[i], nil)
				}
			}

			cfg := &config.Config{
				Agent: config.AgentConfig{
					MaxConcurrentTools: tt.maxConcurrentTool,
				},
			}

			session := &AgentSession{
				toolService: mockToolService,
				config:      cfg,
			}

			results := session.executeToolCallsParallel(tt.toolCalls)

			if len(results) != tt.expectedCount {
				t.Errorf("Expected %d results, got %d", tt.expectedCount, len(results))
			}

			for i, result := range results {
				if i < len(tt.expectedRoles) && result.Role != tt.expectedRoles[i] {
					t.Errorf("Expected result[%d].Role to be %s, got %s", i, tt.expectedRoles[i], result.Role)
				}
			}

			if len(tt.toolCalls) > 0 {
				expectedCallCount := len(tt.toolCalls)
				if mockToolService.ExecuteToolCallCount() != expectedCallCount {
					t.Errorf("Expected ExecuteTool to be called %d times, got %d", expectedCallCount, mockToolService.ExecuteToolCallCount())
				}
			}
		})
	}
}

func TestProcessSyncResponseParallel(t *testing.T) {
	tests := []struct {
		name                  string
		response              *domain.ChatSyncResponse
		maxConcurrentTools    int
		expectedMessageCount  int
		expectedToolCallCount int
	}{
		{
			name: "response with content only",
			response: &domain.ChatSyncResponse{
				RequestID: "req_1",
				Content:   "Hello, world!",
				ToolCalls: []sdk.ChatCompletionMessageToolCall{},
			},
			maxConcurrentTools:    5,
			expectedMessageCount:  1,
			expectedToolCallCount: 0,
		},
		{
			name: "response with tool calls",
			response: &domain.ChatSyncResponse{
				RequestID: "req_1",
				Content:   "I'll help you with that.",
				ToolCalls: []sdk.ChatCompletionMessageToolCall{
					{
						Id: "call_1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Read",
							Arguments: `{"file_path": "test.txt"}`,
						},
					},
					{
						Id: "call_2",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Grep",
							Arguments: `{"pattern": "test"}`,
						},
					},
				},
			},
			maxConcurrentTools:    2,
			expectedMessageCount:  4,
			expectedToolCallCount: 2,
		},
		{
			name: "empty response",
			response: &domain.ChatSyncResponse{
				RequestID: "req_1",
				Content:   "",
				ToolCalls: []sdk.ChatCompletionMessageToolCall{},
			},
			maxConcurrentTools:    5,
			expectedMessageCount:  0,
			expectedToolCallCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockToolService := &domainmocks.FakeToolService{}
			mockToolService.ExecuteToolReturns(&domain.ToolExecutionResult{
				ToolName: "TestTool",
				Success:  true,
				Data:     "test result",
			}, nil)

			cfg := &config.Config{
				Agent: config.AgentConfig{
					MaxConcurrentTools: tt.maxConcurrentTools,
				},
			}

			session := &AgentSession{
				toolService:  mockToolService,
				config:       cfg,
				conversation: []ConversationMessage{},
			}

			err := session.processSyncResponse(tt.response, "request_123")

			if err != nil {
				t.Errorf("processSyncResponse() returned error: %v", err)
			}

			if len(session.conversation) != tt.expectedMessageCount {
				t.Errorf("Expected %d messages in conversation, got %d", tt.expectedMessageCount, len(session.conversation))
			}

			if mockToolService.ExecuteToolCallCount() != tt.expectedToolCallCount {
				t.Errorf("Expected ExecuteTool to be called %d times, got %d", tt.expectedToolCallCount, mockToolService.ExecuteToolCallCount())
			}
		})
	}
}

func mockTime() time.Time {
	return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
}

func TestConvertFromConversationEntry(t *testing.T) {
	session := &AgentSession{
		model: "openai/gpt-4",
	}

	tests := getConvertFromConversationEntryTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := session.convertFromConversationEntry(tt.entry)

			if result.Role != tt.expected.Role {
				t.Errorf("Role = %v, want %v", result.Role, tt.expected.Role)
			}

			if result.Content != tt.expected.Content {
				t.Errorf("Content = %v, want %v", result.Content, tt.expected.Content)
			}

			if !result.Timestamp.Equal(tt.expected.Timestamp) {
				t.Errorf("Timestamp = %v, want %v", result.Timestamp, tt.expected.Timestamp)
			}

			if result.Internal != tt.expected.Internal {
				t.Errorf("Internal = %v, want %v", result.Internal, tt.expected.Internal)
			}

			if result.ToolCallID != tt.expected.ToolCallID {
				t.Errorf("ToolCallID = %v, want %v", result.ToolCallID, tt.expected.ToolCallID)
			}

			if len(result.Images) != len(tt.expected.Images) {
				t.Errorf("Images length = %v, want %v", len(result.Images), len(tt.expected.Images))
			}

			if tt.expected.ToolCalls != nil {
				if result.ToolCalls == nil {
					t.Error("ToolCalls is nil, expected non-nil")
				} else if len(*result.ToolCalls) != len(*tt.expected.ToolCalls) {
					t.Errorf("ToolCalls length = %v, want %v", len(*result.ToolCalls), len(*tt.expected.ToolCalls))
				}
			}

			validateToolExecution(t, result.ToolExecution, tt.expected.ToolExecution)
		})
	}
}

func getConvertFromConversationEntryTestCases() []struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	var tests []struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}

	tests = append(tests, getUserMessageWithPlainTextTestCase())
	tests = append(tests, getAssistantMessageWithToolCallsTestCase())
	tests = append(tests, getToolResponseWithToolCallIDTestCase())
	tests = append(tests, getMessageWithImagesTestCase())
	tests = append(tests, getInternalMessageTestCase())
	tests = append(tests, getMessageWithToolExecutionMetadataTestCase())

	return tests
}

func getUserMessageWithPlainTextTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "user message with plain text",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Hello, how are you?"),
			},
			Model:  "openai/gpt-4",
			Time:   mockTime(),
			Hidden: false,
		},
		expected: ConversationMessage{
			Role:      "user",
			Content:   "Hello, how are you?",
			Timestamp: mockTime(),
			Internal:  false,
		},
	}
}

func getAssistantMessageWithToolCallsTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "assistant message with tool calls",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(""),
				ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
					{
						Id: "call_1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Read",
							Arguments: `{"file_path":"test.txt"}`,
						},
					},
				},
			},
			Model: "openai/gpt-4",
			Time:  mockTime(),
		},
		expected: ConversationMessage{
			Role:      "assistant",
			Content:   "",
			Timestamp: mockTime(),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{
					Id: "call_1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Read",
						Arguments: `{"file_path":"test.txt"}`,
					},
				},
			},
		},
	}
}

func getToolResponseWithToolCallIDTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "tool response with tool_call_id",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("File content here"),
				ToolCallId: stringPtr("call_1"),
			},
			Model: "openai/gpt-4",
			Time:  mockTime(),
		},
		expected: ConversationMessage{
			Role:       "tool",
			Content:    "File content here",
			Timestamp:  mockTime(),
			ToolCallID: "call_1",
		},
	}
}

func getMessageWithImagesTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "message with images",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Check this image"),
			},
			Model: "openai/gpt-4",
			Time:  mockTime(),
			Images: []domain.ImageAttachment{
				{
					Filename: "screenshot.png",
					MimeType: "image/png",
					Data:     "base64data",
				},
			},
		},
		expected: ConversationMessage{
			Role:      "user",
			Content:   "Check this image",
			Timestamp: mockTime(),
			Images: []domain.ImageAttachment{
				{
					Filename: "screenshot.png",
					MimeType: "image/png",
					Data:     "base64data",
				},
			},
		},
	}
}

func getInternalMessageTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "internal message",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Continue working"),
			},
			Model:  "openai/gpt-4",
			Time:   mockTime(),
			Hidden: true,
		},
		expected: ConversationMessage{
			Role:      "user",
			Content:   "Continue working",
			Timestamp: mockTime(),
			Internal:  true,
		},
	}
}

func getMessageWithToolExecutionMetadataTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "message with tool execution metadata",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("Result data"),
				ToolCallId: stringPtr("call_2"),
			},
			Model: "openai/gpt-4",
			Time:  mockTime(),
			ToolExecution: &domain.ToolExecutionResult{
				ToolName: "Read",
				Success:  true,
				Data:     "file content",
			},
		},
		expected: ConversationMessage{
			Role:       "tool",
			Content:    "Result data",
			Timestamp:  mockTime(),
			ToolCallID: "call_2",
			ToolExecution: &domain.ToolExecutionResult{
				ToolName: "Read",
				Success:  true,
				Data:     "file content",
			},
		},
	}
}

func validateToolExecution(t *testing.T, actual, expected *domain.ToolExecutionResult) {
	t.Helper()
	if expected != nil {
		if actual == nil {
			t.Error("ToolExecution is nil, expected non-nil")
			return
		}
		if actual.ToolName != expected.ToolName {
			t.Errorf("ToolExecution.ToolName = %v, want %v", actual.ToolName, expected.ToolName)
		}
		if actual.Success != expected.Success {
			t.Errorf("ToolExecution.Success = %v, want %v", actual.Success, expected.Success)
		}
	}
}

func stringPtr(s string) *string {
	return &s
}
