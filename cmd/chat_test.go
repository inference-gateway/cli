package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"
)

func TestHandleChatCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           string
		initialConv     []sdk.Message
		selectedModel   string
		availableModels []string
		expectedHandled bool
		expectedConvLen int
	}{
		{
			name:            "clear command should clear conversation",
			input:           "/clear",
			initialConv:     []sdk.Message{{Role: sdk.User, Content: "test"}},
			selectedModel:   "gpt-3.5-turbo",
			availableModels: []string{"gpt-3.5-turbo"},
			expectedHandled: true,
			expectedConvLen: 0,
		},
		{
			name:            "history command should be handled",
			input:           "/history",
			initialConv:     []sdk.Message{{Role: sdk.User, Content: "test"}},
			selectedModel:   "gpt-3.5-turbo",
			availableModels: []string{"gpt-3.5-turbo"},
			expectedHandled: true,
			expectedConvLen: 1,
		},
		{
			name:            "models command should be handled",
			input:           "/models",
			initialConv:     []sdk.Message{},
			selectedModel:   "gpt-3.5-turbo",
			availableModels: []string{"gpt-3.5-turbo", "gpt-4"},
			expectedHandled: true,
			expectedConvLen: 0,
		},
		{
			name:            "help command should be handled",
			input:           "/help",
			initialConv:     []sdk.Message{},
			selectedModel:   "gpt-3.5-turbo",
			availableModels: []string{"gpt-3.5-turbo"},
			expectedHandled: true,
			expectedConvLen: 0,
		},
		{
			name:            "question mark help should be handled",
			input:           "?",
			initialConv:     []sdk.Message{},
			selectedModel:   "gpt-3.5-turbo",
			availableModels: []string{"gpt-3.5-turbo"},
			expectedHandled: true,
			expectedConvLen: 0,
		},
		{
			name:            "compact command should be handled",
			input:           "/compact",
			initialConv:     []sdk.Message{},
			selectedModel:   "gpt-3.5-turbo",
			availableModels: []string{"gpt-3.5-turbo"},
			expectedHandled: true,
			expectedConvLen: 0,
		},
		{
			name:            "unknown command should be handled with error",
			input:           "/unknown",
			initialConv:     []sdk.Message{},
			selectedModel:   "gpt-3.5-turbo",
			availableModels: []string{"gpt-3.5-turbo"},
			expectedHandled: true,
			expectedConvLen: 0,
		},
		{
			name:            "regular text should not be handled as command",
			input:           "Hello world",
			initialConv:     []sdk.Message{},
			selectedModel:   "gpt-3.5-turbo",
			availableModels: []string{"gpt-3.5-turbo"},
			expectedHandled: false,
			expectedConvLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conversation := make([]sdk.Message, len(tt.initialConv))
			copy(conversation, tt.initialConv)
			selectedModel := tt.selectedModel

			handled := handleChatCommands(tt.input, &conversation, &selectedModel, tt.availableModels)

			if handled != tt.expectedHandled {
				t.Errorf("handleChatCommands() handled = %v, want %v", handled, tt.expectedHandled)
			}

			if len(conversation) != tt.expectedConvLen {
				t.Errorf("conversation length = %v, want %v", len(conversation), tt.expectedConvLen)
			}
		})
	}
}

func TestProcessFileReferences(t *testing.T) {
	t.Parallel()

	tempDir, err := os.MkdirTemp("", "chat_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	testFile := filepath.Join(tempDir, "test.txt")
	testContent := "This is test content"
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name        string
		input       string
		wantErr     bool
		expectsFile bool
		description string
	}{
		{
			name:        "no file references",
			input:       "Hello world",
			wantErr:     false,
			expectsFile: false,
			description: "Input without file references should pass through unchanged",
		},
		{
			name:        "single file reference - existing file",
			input:       "Look at @" + testFile + " please",
			wantErr:     false,
			expectsFile: true,
			description: "Input with @filename should read existing file",
		},
		{
			name:        "single file reference - nonexistent file",
			input:       "Look at @nonexistent.yaml please",
			wantErr:     true,
			expectsFile: true,
			description: "Input with @filename should error for missing file",
		},
		{
			name:        "file reference with relative path",
			input:       "Check @./nonexistent.go",
			wantErr:     true,
			expectsFile: true,
			description: "File references with paths should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processFileReferences(tt.input)

			if tt.wantErr && err == nil {
				t.Errorf("processFileReferences() expected error but got none")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("processFileReferences() unexpected error: %v", err)
			}

			if !tt.expectsFile && result != tt.input {
				t.Errorf("processFileReferences() = %v, want %v", result, tt.input)
			}

			if !tt.wantErr && tt.expectsFile {
				if !containsText(result, testContent) {
					t.Errorf("processFileReferences() result should contain file content")
				}
			}
		})
	}
}

func TestReadFileForChat(t *testing.T) {
	t.Parallel()

	tempDir, err := os.MkdirTemp("", "chat_file_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	validFile := filepath.Join(tempDir, "valid.txt")
	testContent := "Hello, world!\nThis is a test file."
	err = os.WriteFile(validFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create valid test file: %v", err)
	}

	largeFile := filepath.Join(tempDir, "large.txt")
	largeContent := make([]byte, 101*1024) // 101KB
	for i := range largeContent {
		largeContent[i] = 'A'
	}
	err = os.WriteFile(largeFile, largeContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create large test file: %v", err)
	}

	tests := []struct {
		name        string
		filePath    string
		wantErr     bool
		expectEmpty bool
	}{
		{
			name:     "valid file",
			filePath: validFile,
			wantErr:  false,
		},
		{
			name:     "nonexistent file",
			filePath: filepath.Join(tempDir, "nonexistent.txt"),
			wantErr:  true,
		},
		{
			name:     "directory instead of file",
			filePath: tempDir,
			wantErr:  true,
		},
		{
			name:     "file too large",
			filePath: largeFile,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := readFileForChat(tt.filePath)

			if tt.wantErr && err == nil {
				t.Errorf("readFileForChat() expected error but got none")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("readFileForChat() unexpected error: %v", err)
			}

			if !tt.wantErr && !tt.expectEmpty && content == "" {
				t.Errorf("readFileForChat() expected content but got empty string")
			}

			if !tt.wantErr && tt.filePath == validFile && content != testContent {
				t.Errorf("readFileForChat() = %q, want %q", content, testContent)
			}
		})
	}
}

func TestFormatConversationForDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		conversation  []sdk.Message
		selectedModel string
		expectedLen   int
		expectRoles   []string
	}{
		{
			name:          "empty conversation",
			conversation:  []sdk.Message{},
			selectedModel: "gpt-3.5-turbo",
			expectedLen:   0,
			expectRoles:   []string{},
		},
		{
			name: "single user message",
			conversation: []sdk.Message{
				{Role: sdk.User, Content: "Hello"},
			},
			selectedModel: "gpt-3.5-turbo",
			expectedLen:   1,
			expectRoles:   []string{"👤 You"},
		},
		{
			name: "user and assistant messages",
			conversation: []sdk.Message{
				{Role: sdk.User, Content: "Hello"},
				{Role: sdk.Assistant, Content: "Hi there!"},
			},
			selectedModel: "gpt-3.5-turbo",
			expectedLen:   2,
			expectRoles:   []string{"👤 You", "🤖 gpt-3.5-turbo"},
		},
		{
			name: "system message",
			conversation: []sdk.Message{
				{Role: sdk.System, Content: "You are a helpful assistant"},
			},
			selectedModel: "gpt-4",
			expectedLen:   1,
			expectRoles:   []string{"⚙️ System"},
		},
		{
			name: "tool message",
			conversation: []sdk.Message{
				{Role: sdk.Tool, Content: "Tool result"},
			},
			selectedModel: "gpt-4",
			expectedLen:   1,
			expectRoles:   []string{"🔧 Tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatConversationForDisplay(tt.conversation, tt.selectedModel)

			if len(result) != tt.expectedLen {
				t.Errorf("formatConversationForDisplay() length = %v, want %v", len(result), tt.expectedLen)
			}

			for i, expectedRole := range tt.expectRoles {
				if i < len(result) {
					if !containsRole(result[i], expectedRole) {
						t.Errorf("formatConversationForDisplay()[%d] = %v, expected to contain %v", i, result[i], expectedRole)
					}
				}
			}
		})
	}
}

func TestFormatMetricsString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metrics  *ChatMetrics
		expected string
		contains []string
	}{
		{
			name:     "nil metrics",
			metrics:  nil,
			expected: "",
		},
		{
			name: "metrics with duration only",
			metrics: &ChatMetrics{
				Duration: 1000000000, // 1 second in nanoseconds
			},
			contains: []string{"Time: 1s"},
		},
		{
			name: "metrics with usage",
			metrics: &ChatMetrics{
				Duration: 500000000, // 500ms
				Usage: &sdk.CompletionUsage{
					PromptTokens:     100,
					CompletionTokens: 50,
					TotalTokens:      150,
				},
			},
			contains: []string{"Time:", "Input: 100 tokens", "Output: 50 tokens", "Total: 150 tokens"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMetricsString(tt.metrics)

			if tt.expected != "" && result != tt.expected {
				t.Errorf("formatMetricsString() = %v, want %v", result, tt.expected)
			}

			for _, contains := range tt.contains {
				if result == "" || !containsText(result, contains) {
					t.Errorf("formatMetricsString() = %v, expected to contain %v", result, contains)
				}
			}
		})
	}
}

func TestCompactConversation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "compact_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	tests := []struct {
		name         string
		conversation []sdk.Message
		model        string
		expectErr    bool
		expectFile   bool
	}{
		{
			name:         "empty conversation",
			conversation: []sdk.Message{},
			model:        "gpt-3.5-turbo",
			expectErr:    false,
			expectFile:   false,
		},
		{
			name: "single message conversation",
			conversation: []sdk.Message{
				{Role: sdk.User, Content: "Hello world"},
			},
			model:      "gpt-4",
			expectErr:  false,
			expectFile: true,
		},
		{
			name: "multi-message conversation with tool calls",
			conversation: []sdk.Message{
				{Role: sdk.User, Content: "What's the weather?"},
				{
					Role:    sdk.Assistant,
					Content: "I'll check the weather for you.",
					ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
						{
							Id:   "call_123",
							Type: "function",
							Function: sdk.ChatCompletionMessageToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"location": "New York"}`,
							},
						},
					},
				},
				{Role: sdk.Tool, Content: "Sunny, 75°F", ToolCallId: &[]string{"call_123"}[0]},
				{Role: sdk.Assistant, Content: "It's sunny and 75°F in New York!"},
			},
			model:      "gpt-4",
			expectErr:  false,
			expectFile: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTempDir, err := os.MkdirTemp(tempDir, "test_*")
			if err != nil {
				t.Fatalf("Failed to create test temp dir: %v", err)
			}

			originalDir, _ := os.Getwd()
			configFile := filepath.Join(testTempDir, ".infer", "config.yaml")
			err = os.MkdirAll(filepath.Dir(configFile), 0755)
			if err != nil {
				t.Fatalf("Failed to create config dir: %v", err)
			}

			configData := `compact:
  output_dir: "` + testTempDir + `"`

			err = os.WriteFile(configFile, []byte(configData), 0644)
			if err != nil {
				t.Fatalf("Failed to create config file: %v", err)
			}

			err = os.Chdir(testTempDir)
			if err != nil {
				t.Fatalf("Failed to change directory: %v", err)
			}
			defer func() {
				if chdirErr := os.Chdir(originalDir); chdirErr != nil {
					t.Errorf("Failed to restore original directory: %v", chdirErr)
				}
			}()

			err = compactConversation(tt.conversation, tt.model)

			if tt.expectErr && err == nil {
				t.Errorf("compactConversation() expected error but got none")
			}

			if !tt.expectErr && err != nil {
				t.Errorf("compactConversation() unexpected error: %v", err)
			}

			if tt.expectFile {
				files, err := filepath.Glob(filepath.Join(testTempDir, "chat-export-*.md"))
				if err != nil {
					t.Errorf("Failed to list exported files: %v", err)
				}
				if len(files) == 0 {
					t.Errorf("Expected export file to be created but none found")
				} else {
					content, err := os.ReadFile(files[0])
					if err != nil {
						t.Errorf("Failed to read export file: %v", err)
					} else {
						contentStr := string(content)
						if !strings.Contains(contentStr, "# Chat Session Export") {
							t.Errorf("Export file should contain header")
						}
						if !strings.Contains(contentStr, tt.model) {
							t.Errorf("Export file should contain model name")
						}
						if len(tt.conversation) > 0 && !strings.Contains(contentStr, tt.conversation[0].Content) {
							t.Errorf("Export file should contain conversation content")
						}
					}
				}
			}
		})
	}

	if err := os.Chdir(originalDir); err != nil {
		t.Errorf("Failed to restore original directory after all tests: %v", err)
	}
}

// Helper functions
func containsRole(text, role string) bool {
	return len(text) > 0 && text[:len(role)] == role
}

func containsText(text, substring string) bool {
	return len(text) >= len(substring) &&
		text != "" && substring != "" &&
		contains(text, substring)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestChatRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  ChatRequest
		expected ChatRequest
	}{
		{
			name: "basic chat request",
			request: ChatRequest{
				Model: "gpt-3.5-turbo",
				Messages: []ChatMessage{
					{Role: sdk.User, Content: "Hello"},
				},
				Stream: false,
			},
			expected: ChatRequest{
				Model: "gpt-3.5-turbo",
				Messages: []ChatMessage{
					{Role: sdk.User, Content: "Hello"},
				},
				Stream: false,
			},
		},
		{
			name: "streaming chat request",
			request: ChatRequest{
				Model: "gpt-4",
				Messages: []ChatMessage{
					{Role: sdk.System, Content: "You are helpful"},
					{Role: sdk.User, Content: "Hi"},
				},
				Stream: true,
			},
			expected: ChatRequest{
				Model: "gpt-4",
				Messages: []ChatMessage{
					{Role: sdk.System, Content: "You are helpful"},
					{Role: sdk.User, Content: "Hi"},
				},
				Stream: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.request, tt.expected) {
				t.Errorf("ChatRequest = %v, want %v", tt.request, tt.expected)
			}
		})
	}
}
