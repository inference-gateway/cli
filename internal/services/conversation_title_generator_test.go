package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"

	generated "github.com/inference-gateway/cli/tests/mocks/generated"
)

func TestConversationTitleGenerator_GenerateTitleForConversation(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		entries        []domain.ConversationEntry
		aiResponse     string
		expectedTitle  string
		expectError    bool
		expectFallback bool
	}{
		{
			name:    "disabled generator",
			enabled: false,
			entries: []domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: "Hello world"}, Time: time.Now()},
			},
			expectedTitle: "",
			expectError:   false,
		},
		{
			name:        "empty conversation",
			enabled:     true,
			entries:     []domain.ConversationEntry{},
			expectError: false,
		},
		{
			name:    "successful title generation",
			enabled: true,
			entries: []domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: "How do I set up Docker?"}, Time: time.Now()},
				{Message: sdk.Message{Role: sdk.Assistant, Content: "I'll help you set up Docker..."}, Time: time.Now()},
			},
			aiResponse:    "Docker Setup Guide",
			expectedTitle: "Docker Setup Guide",
			expectError:   false,
		},
		{
			name:    "AI generation fails",
			enabled: true,
			entries: []domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: "Help me understand how to properly configure and deploy a React application using Docker containers"}, Time: time.Now()},
			},
			aiResponse:     "",
			expectedTitle:  "",
			expectError:    true,
			expectFallback: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStorage := &generated.FakeConversationStorage{}
			mockClient := &generated.FakeSDKClient{}

			cfg := &config.Config{
				Conversation: config.ConversationConfig{
					TitleGeneration: config.ConversationTitleConfig{
						Enabled: tt.enabled,
						Model:   "anthropic/claude-3-haiku",
					},
				},
				Agent: config.AgentConfig{
					Model: "anthropic/claude-3-haiku",
				},
			}

			generator := NewConversationTitleGeneratorWithSDKClient(mockClient, mockStorage, cfg)

			conversationID := "test-conv-123"
			metadata := storage.ConversationMetadata{
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
							{Message: sdk.Message{Content: tt.aiResponse}},
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
		entries  []domain.ConversationEntry
		expected string
	}{
		{
			name: "first user message under 10 words",
			entries: []domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: "Help me with Docker"}, Time: time.Now()},
			},
			expected: "Help me with Docker",
		},
		{
			name: "first user message over 10 words",
			entries: []domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: "Please help me understand how to properly configure and deploy a complex React application using Docker containers"}, Time: time.Now()},
			},
			expected: "Please help me understand how to properly",
		},
		{
			name: "long title truncated at 50 chars",
			entries: []domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: "This is a very long message that should be truncated when used as a fallback title because it exceeds the fifty character limit"}, Time: time.Now()},
			},
			expected: "This is a very long message that should be",
		},
		{
			name:     "empty entries",
			entries:  []domain.ConversationEntry{},
			expected: "Conversation",
		},
		{
			name: "system reminder ignored",
			entries: []domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.System, Content: "System reminder"}, IsSystemReminder: true, Time: time.Now()},
				{Message: sdk.Message{Role: sdk.User, Content: "Real user message"}, Time: time.Now()},
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
	entries := []domain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.System, Content: "System message"}, IsSystemReminder: true, Time: time.Now()},
		{Message: sdk.Message{Role: sdk.User, Content: "How do I deploy a React app?"}, Time: time.Now()},
		{Message: sdk.Message{Role: sdk.Assistant, Content: "I'll help you deploy your React application. There are several approaches..."}, Time: time.Now()},
		{Message: sdk.Message{Role: sdk.User, Content: "What about using Docker?"}, Time: time.Now()},
	}

	generator := &ConversationTitleGenerator{}
	result := generator.formatConversationForTitleGeneration(entries)

	assert.Contains(t, result, "User: How do I deploy a React app?")
	assert.Contains(t, result, "Assistant: I'll help you deploy your React application. There are several approaches...")
	assert.Contains(t, result, "User: What about using Docker?")
	assert.NotContains(t, result, "System message")

	assert.True(t, len(result) <= 2000, "Formatted content should not exceed 2000 characters")
}
