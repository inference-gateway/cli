package services

import (
	"testing"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	"github.com/stretchr/testify/assert"
)

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
			expected: "Please help me understand how to properly configure and deploy a complex",
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