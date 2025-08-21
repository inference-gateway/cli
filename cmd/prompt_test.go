package cmd

import (
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

func TestIsModelAvailable(t *testing.T) {
	models := []string{"openai/gpt-4", "anthropic/claude-3", "openai/gpt-3.5-turbo"}

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
	session := &PromptSession{
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

	if messages[0].Content != "Hello" {
		t.Errorf("Expected first message content to be 'Hello', got %s", messages[0].Content)
	}
}

func TestIsTaskComplete(t *testing.T) {
	tests := []struct {
		name         string
		conversation []ConversationMessage
		expected     bool
	}{
		{
			name: "Task completed - direct indicator",
			conversation: []ConversationMessage{
				{Role: "user", Content: "Fix issue 38"},
				{Role: "assistant", Content: "The task is complete and the issue has been fixed."},
			},
			expected: true,
		},
		{
			name: "Task not completed - no indicators",
			conversation: []ConversationMessage{
				{Role: "user", Content: "Fix issue 38"},
				{Role: "assistant", Content: "I'm working on this issue."},
			},
			expected: false,
		},
		{
			name:         "Empty conversation",
			conversation: []ConversationMessage{},
			expected:     false,
		},
		{
			name: "Last message is not assistant",
			conversation: []ConversationMessage{
				{Role: "user", Content: "Fix issue 38"},
				{Role: "tool", Content: "Some tool result"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &PromptSession{
				conversation: tt.conversation,
			}

			result := session.isTaskComplete()
			if result != tt.expected {
				t.Errorf("isTaskComplete() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func mockTime() time.Time {
	return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
}
