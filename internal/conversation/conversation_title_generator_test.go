package conversation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	assert "github.com/stretchr/testify/assert"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"
	storagemocks "github.com/inference-gateway/cli/tests/mocks/storage"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func TestConversationTitleGenerator_GenerateTitleForConversation(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		entries        []convdomain.ConversationEntry
		aiResponse     string
		expectedTitle  string
		expectError    bool
		expectFallback bool
	}{
		{
			name:    "disabled generator",
			enabled: false,
			entries: []convdomain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Hello world")}, Time: time.Now()},
			},
			expectedTitle: "",
			expectError:   false,
		},
		{
			name:        "empty conversation",
			enabled:     true,
			entries:     []convdomain.ConversationEntry{},
			expectError: false,
		},
		{
			name:    "successful title generation",
			enabled: true,
			entries: []convdomain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("How do I set up Docker?")}, Time: time.Now()},
				{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("I'll help you set up Docker...")}, Time: time.Now()},
			},
			aiResponse:    "Docker Setup Guide",
			expectedTitle: "Docker Setup Guide",
			expectError:   false,
		},
		{
			name:    "AI generation fails",
			enabled: true,
			entries: []convdomain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Help me understand how to properly configure and deploy a React application using Docker containers")}, Time: time.Now()},
			},
			aiResponse:     "",
			expectedTitle:  "",
			expectError:    true,
			expectFallback: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStorage := &storagemocks.FakeConversationStorage{}
			mockClient := &sdkmocks.FakeClient{}

			cfg := &config.Config{
				Conversation: config.ConversationConfig{
					TitleGeneration: config.ConversationTitleConfig{
						Enabled: tt.enabled,
						Model:   "anthropic/claude-4-haiku",
					},
				},
				Agent: config.AgentConfig{
					Model: "anthropic/claude-4-haiku",
				},
			}

			generator := NewConversationTitleGenerator(mockClient, mockStorage, cfg)

			conversationID := "test-conv-123"
			metadata := convdomain.ConversationMetadata{
				ID:           conversationID,
				Title:        "Original Title",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				MessageCount: len(tt.entries),
			}

			if tt.enabled {
				mockStorage.LoadConversationReturns(tt.entries, metadata, nil)
			}

			if tt.enabled && len(tt.entries) > 0 {
				mockClient.WithOptionsReturns(mockClient)
				mockClient.WithMiddlewareOptionsReturns(mockClient)

				if tt.aiResponse != "" {
					mockResponse := &sdk.CreateChatCompletionResponse{
						Choices: []sdk.ChatCompletionChoice{
							{Message: sdk.Message{Content: sdk.NewMessageContent(tt.aiResponse)}},
						},
					}
					mockClient.GenerateContentReturns(mockResponse, nil)
					mockStorage.UpdateConversationMetadataReturns(nil)
				} else {
					mockClient.GenerateContentReturns(nil, fmt.Errorf("AI generation failed"))
				}
			}

			err := generator.GenerateTitleForConversation(context.Background(), conversationID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

		})
	}
}

func TestConversationTitleGenerator_fallbackTitle(t *testing.T) {
	tests := []struct {
		name     string
		entries  []convdomain.ConversationEntry
		expected string
	}{
		{
			name: "first user message under 10 words",
			entries: []convdomain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Help me with Docker")}, Time: time.Now()},
			},
			expected: "Help me with Docker",
		},
		{
			name: "first user message over 10 words",
			entries: []convdomain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Please help me understand how to properly configure and deploy a complex React application using Docker containers")}, Time: time.Now()},
			},
			expected: "Please help me understand how to properly",
		},
		{
			name: "long title truncated at 50 chars",
			entries: []convdomain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("This is a very long message that should be truncated when used as a fallback title because it exceeds the fifty character limit")}, Time: time.Now()},
			},
			expected: "This is a very long message that should be",
		},
		{
			name:     "empty entries",
			entries:  []convdomain.ConversationEntry{},
			expected: "Conversation",
		},
		{
			name: "system reminder ignored",
			entries: []convdomain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.System, Content: sdk.NewMessageContent("System reminder")}, Hidden: true, Time: time.Now()},
				{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Real user message")}, Time: time.Now()},
			},
			expected: "Real user message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := &ConversationTitleGenerator{}
			result := generator.fallbackTitle(tt.entries)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConversationTitleGenerator_formatConversationForTitleGeneration(t *testing.T) {
	entries := []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.System, Content: sdk.NewMessageContent("System message")}, Hidden: true, Time: time.Now()},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("How do I deploy a React app?")}, Time: time.Now()},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("I'll help you deploy your React application. There are several approaches...")}, Time: time.Now()},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("What about using Docker?")}, Time: time.Now()},
	}

	generator := &ConversationTitleGenerator{}
	result := generator.formatConversationForTitleGeneration(entries)

	assert.Contains(t, result, "User: How do I deploy a React app?")
	assert.Contains(t, result, "Assistant: I'll help you deploy your React application. There are several approaches...")
	assert.Contains(t, result, "User: What about using Docker?")
	assert.NotContains(t, result, "System message")

	assert.True(t, len(result) <= 2000, "Formatted content should not exceed 2000 characters")
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "short title is kept", title: "Fix the login bug", want: "Fix the login bug"},
		{name: "long title ends at its last word break", title: strings.Repeat("word ", 12), want: strings.TrimSpace(strings.Repeat("word ", 10))},
		{name: "multibyte runes are never split", title: strings.Repeat("é", 60), want: strings.Repeat("é", 50)},
		{name: "an early word break counts runes, not bytes", title: strings.Repeat("é", 20) + " " + strings.Repeat("é", 40), want: strings.Repeat("é", 20) + " " + strings.Repeat("é", 29)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateTitle(tt.title)
			assert.Equal(t, tt.want, got)
			assert.True(t, utf8.ValidString(got))
		})
	}
}
