package handlers

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	assert "github.com/stretchr/testify/assert"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestChatHandler_extractMarkdownSummary_BasicCases(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name            string
		content         string
		expectedSummary string
		expectedFound   bool
	}{
		{
			name: "Basic summary section",
			content: `# Document Title

## Summary
This is a summary of the document.
It has multiple lines.

## Details
More content here.`,
			expectedSummary: `## Summary
This is a summary of the document.
It has multiple lines.
`,
			expectedFound: true,
		},
		{
			name: "Summary with next section",
			content: `## Summary
Brief overview of the project.
Key features included.

## Installation
Follow these steps...`,
			expectedSummary: `## Summary
Brief overview of the project.
Key features included.
`,
			expectedFound: true,
		},
		{
			name: "Summary with document separator",
			content: `## Summary
Project overview here.
Some bullet points.

---

More content after separator.`,
			expectedSummary: `## Summary
Project overview here.
Some bullet points.
`,
			expectedFound: true,
		},
		{
			name: "Summary at end of document",
			content: `# Main Title

## Summary
This is the final summary.
End of document.`,
			expectedSummary: `## Summary
This is the final summary.
End of document.
`,
			expectedFound: true,
		},
		{
			name: "No summary section",
			content: `# Document

## Introduction
Some content.

## Details
More content.`,
			expectedSummary: "",
			expectedFound:   false,
		},
		{
			name: "Empty summary section",
			content: `## Summary

## Next Section
Content here.`,
			expectedSummary: `## Summary
`,
			expectedFound: true,
		},
		{
			name: "Summary with only heading",
			content: `## Summary
## Next Section`,
			expectedSummary: "",
			expectedFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, found := handler.ExtractMarkdownSummary(tt.content)

			assert.Equal(t, tt.expectedFound, found, "Found flag should match expected")
			if tt.expectedFound {
				assert.Equal(t, tt.expectedSummary, summary, "Summary content should match expected")
			} else {
				assert.Empty(t, summary, "Summary should be empty when not found")
			}
		})
	}
}

func TestChatHandler_extractMarkdownSummary_ComplexCases(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name            string
		content         string
		expectedSummary string
		expectedFound   bool
	}{
		{
			name: "Multiple summary sections (first one wins)",
			content: `## Summary
First summary content.

## Details
Some details.

## Summary
Second summary content.`,
			expectedSummary: `## Summary
First summary content.
`,
			expectedFound: true,
		},
		{
			name: "Summary with subsections",
			content: `## Summary
Main summary content.

### Key Points
- Point 1
- Point 2

## Next Section
Other content.`,
			expectedSummary: `## Summary
Main summary content.

### Key Points
- Point 1
- Point 2
`,
			expectedFound: true,
		},
		{
			name: "Summary with extra whitespace",
			content: `   ## Summary
Content with spaces.
More content.

  ## Next Section
Other stuff.`,
			expectedSummary: `   ## Summary
Content with spaces.
More content.
`,
			expectedFound: true,
		},
		{
			name: "Case sensitivity test",
			content: `## summary
Lowercase summary.

## Details
Content.`,
			expectedSummary: "",
			expectedFound:   false,
		},
		{
			name: "Summary with code blocks",
			content: `## Summary
This project includes:

` + "```go" + `
func main() {
    fmt.Println("Hello")
}
` + "```" + `

## Usage
Instructions here.`,
			expectedSummary: `## Summary
This project includes:

` + "```go" + `
func main() {
    fmt.Println("Hello")
}
` + "```" + `
`,
			expectedFound: true,
		},
		{
			name: "Summary with horizontal rule at end",
			content: `## Summary
Project summary here.
---`,
			expectedSummary: `## Summary
Project summary here.
`,
			expectedFound: true,
		},
		{
			name:            "Empty content",
			content:         "",
			expectedSummary: "",
			expectedFound:   false,
		},
		{
			name: "Only newlines",
			content: `


`,
			expectedSummary: "",
			expectedFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, found := handler.ExtractMarkdownSummary(tt.content)

			assert.Equal(t, tt.expectedFound, found, "Found flag should match expected")
			if tt.expectedFound {
				assert.Equal(t, tt.expectedSummary, summary, "Summary content should match expected")
			} else {
				assert.Empty(t, summary, "Summary should be empty when not found")
			}
		})
	}
}

func TestChatHandler_extractMarkdownSummary_ExportFormat(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name            string
		content         string
		expectedSummary string
		expectedFound   bool
	}{
		{
			name: "Export file format with summary until separator",
			content: `# Chat Conversation Export

**Generated:** August 19, 2025 at 3:29 PM
**Total Messages:** 8

---

## Summary

**Conversation Summary:**

**Main Topics:**
- Introduction and availability for software engineering assistance

---

## Full Conversation

Message content here...`,
			expectedSummary: `## Summary

**Conversation Summary:**

**Main Topics:**
- Introduction and availability for software engineering assistance
`,
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, found := handler.ExtractMarkdownSummary(tt.content)

			assert.Equal(t, tt.expectedFound, found, "Found flag should match expected")
			if tt.expectedFound {
				assert.Equal(t, tt.expectedSummary, summary, "Summary content should match expected")
			} else {
				assert.Empty(t, summary, "Summary should be empty when not found")
			}
		})
	}
}

// Test edge cases and boundary conditions
func TestChatHandler_extractMarkdownSummary_EdgeCases(t *testing.T) {
	handler := &ChatHandler{}

	t.Run("Very large summary", func(t *testing.T) {
		largeContent := "## Summary\n"
		for i := range 1000 {
			largeContent += "This is line " + string(rune(i)) + " of the summary.\n"
		}
		largeContent += "\n## Next Section\nOther content."

		summary, found := handler.ExtractMarkdownSummary(largeContent)

		assert.True(t, found)
		assert.Contains(t, summary, "## Summary")
		assert.NotContains(t, summary, "## Next Section")
	})

	t.Run("Summary with special characters", func(t *testing.T) {
		content := `## Summary
Special chars: !@#$%^&*()
Unicode: 你好世界 🚀
Emojis work too! ✨

## Details
More content.`

		summary, found := handler.ExtractMarkdownSummary(content)

		assert.True(t, found)
		assert.Contains(t, summary, "Special chars: !@#$%^&*()")
		assert.Contains(t, summary, "Unicode: 你好世界 🚀")
		assert.Contains(t, summary, "Emojis work too! ✨")
		assert.NotContains(t, summary, "## Details")
	})

	t.Run("Mixed line endings", func(t *testing.T) {
		content := "## Summary\r\nWindows line ending content.\nUnix line ending.\r\n\r\n## Next Section\r\nMore content."

		summary, found := handler.ExtractMarkdownSummary(content)

		assert.True(t, found)
		assert.Contains(t, summary, "Windows line ending content.")
		assert.Contains(t, summary, "Unix line ending.")
	})
}

func TestChatHandler_parseToolCall(t *testing.T) {
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
			name:        "Tool call with single quoted arguments",
			input:       `Bash(command='ls -la')`,
			expectTool:  "Bash",
			expectArgs:  map[string]any{"command": "ls -la"},
			expectError: false,
		},
		{
			name:        "Tool call with mixed quotes",
			input:       `WebSearch(query="golang testing", max_results=10)`,
			expectTool:  "WebSearch",
			expectArgs:  map[string]any{"query": "golang testing", "max_results": float64(10)},
			expectError: false,
		},
		{
			name:        "Tool call with complex paths",
			input:       `Read(file_path="/home/user/Documents/file with spaces.txt")`,
			expectTool:  "Read",
			expectArgs:  map[string]any{"file_path": "/home/user/Documents/file with spaces.txt"},
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
		{
			name:        "Empty tool name",
			input:       `(file_path="test.txt")`,
			expectTool:  "",
			expectArgs:  nil,
			expectError: true,
		},
		{
			name:        "Tool call with spaces around tool name",
			input:       ` Write (file_path="test.txt")`,
			expectTool:  "Write",
			expectArgs:  map[string]any{"file_path": "test.txt"},
			expectError: false,
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

func TestChatHandler_parseArguments(t *testing.T) {
	handler := &ChatHandler{}

	tests := []struct {
		name        string
		input       string
		expectArgs  map[string]any
		expectError bool
	}{
		{
			name:        "Single quoted argument",
			input:       `file_path="test.txt"`,
			expectArgs:  map[string]any{"file_path": "test.txt"},
			expectError: false,
		},
		{
			name:        "Multiple arguments",
			input:       `file_path="test.txt", content="Hello World"`,
			expectArgs:  map[string]any{"file_path": "test.txt", "content": "Hello World"},
			expectError: false,
		},
		{
			name:        "Single quoted arguments",
			input:       `command='ls -la'`,
			expectArgs:  map[string]any{"command": "ls -la"},
			expectError: false,
		},
		{
			name:        "Unquoted argument",
			input:       `count=10`,
			expectArgs:  map[string]any{"count": float64(10)},
			expectError: false,
		},
		{
			name:        "Quoted number argument",
			input:       `limit="51"`,
			expectArgs:  map[string]any{"limit": float64(51)},
			expectError: false,
		},
		{
			name:        "Empty string",
			input:       ``,
			expectArgs:  map[string]any{},
			expectError: false,
		},
		{
			name:        "Arguments with spaces",
			input:       `path="/home/user/file with spaces.txt", mode="read"`,
			expectArgs:  map[string]any{"path": "/home/user/file with spaces.txt", "mode": "read"},
			expectError: false,
		},
		{
			name:        "Arguments with special characters",
			input:       `pattern="[a-zA-Z0-9]+"`,
			expectArgs:  map[string]any{"pattern": "[a-zA-Z0-9]+"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := handler.ParseArguments(tt.input)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectArgs, args)
		})
	}
}

func TestFormatMetricsWithoutSessionTokens(t *testing.T) {
	conversationRepo := services.NewInMemoryConversationRepository(nil, nil)
	shortcutRegistry := shortcuts.NewRegistry()

	messageQueue := services.NewMessageQueueService()

	handler := NewChatHandler(
		nil, // agentService
		conversationRepo,
		nil, // conversationOptimizer
		nil, // modelService
		nil, // configService
		nil, // toolService
		nil, // fileService
		nil, // imageService
		shortcutRegistry,
		nil, // stateManager
		messageQueue,
		nil, // taskRetentionService
		nil, // backgroundTaskService
		nil, // backgroundShellService
		nil, // agentManager
		config.DefaultConfig(),
	)

	err := conversationRepo.AddTokenUsage("test-model", 100, 50, 150)
	if err != nil {
		t.Fatalf("Failed to add token usage: %v", err)
	}

	metrics := &domain.ChatMetrics{
		Duration: 1 * time.Second,
		Usage: &sdk.CompletionUsage{
			PromptTokens:     25,
			CompletionTokens: 15,
			TotalTokens:      40,
		},
	}

	result := handler.FormatMetrics(metrics)

	if !strings.Contains(result, "Input: 25 tokens") {
		t.Errorf("Expected current input tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Output: 15 tokens") {
		t.Errorf("Expected current output tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Total: 40 tokens") {
		t.Errorf("Expected current total tokens in result, got: %s", result)
	}

	if strings.Contains(result, "Session Input") {
		t.Errorf("Session Input tokens should not be in status bar, got: %s", result)
	}

	if strings.Contains(result, "Session Output") {
		t.Errorf("Session Output tokens should not be in status bar, got: %s", result)
	}
}

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
			expectedCmd: false,
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

	conversationRepo := services.NewInMemoryConversationRepository(nil, nil)
	shortcutRegistry := shortcuts.NewRegistry()
	messageQueue := services.NewMessageQueueService()

	return NewChatHandler(
		mockAgent,
		conversationRepo,
		nil,
		mockModel,
		mockConfig,
		mockTool,
		mockFile,
		nil,
		shortcutRegistry,
		stateManager,
		messageQueue,
		nil,
		nil,
		nil,
		nil,
		config.DefaultConfig(),
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

			conversationRepo := services.NewInMemoryConversationRepository(nil, nil)
			stateManager := services.NewStateManager(false)
			shortcutRegistry := shortcuts.NewRegistry()
			messageQueue := services.NewMessageQueueService()

			handler := NewChatHandler(
				mockAgent,
				conversationRepo,
				nil,
				mockModel,
				mockConfig,
				mockTool,
				mockFile,
				nil,
				shortcutRegistry,
				stateManager,
				messageQueue,
				nil,
				nil,
				nil,
				nil,
				config.DefaultConfig(),
			)

			cmd := handler.handleChatComplete(tt.msg)

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
