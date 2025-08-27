package services

import (
	"context"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/infra/storage"
	sdk "github.com/inference-gateway/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
			name:    "fallback to first 10 words",
			enabled: true,
			entries: []domain.ConversationEntry{
				{Message: sdk.Message{Role: sdk.User, Content: "Help me understand how to properly configure and deploy a React application using Docker containers"}, Time: time.Now()},
			},
			aiResponse:     "",
			expectedTitle:  "Help me understand how to properly configure and deploy a React",
			expectError:    false,
			expectFallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStorage := &FakeConversationStorage{}
			mockClient := &FakeSDKClient{}

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

			generator := NewConversationTitleGenerator(mockClient, mockStorage, cfg)

			conversationID := "test-conv-123"
			metadata := storage.ConversationMetadata{
				ID:           conversationID,
				Title:        "Original Title",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				MessageCount: len(tt.entries),
			}

			mockStorage.On("LoadConversation", mock.Anything, conversationID).Return(tt.entries, metadata, nil)

			if tt.enabled && len(tt.entries) > 0 && tt.aiResponse != "" {
				mockResponse := &sdk.CreateChatCompletionResponse{
					Choices: []sdk.ChatCompletionChoice{
						{Message: sdk.Message{Content: tt.aiResponse}},
					},
				}
				mockClient.On("WithOptions", mock.Anything).Return(mockClient)
				mockClient.On("WithMiddlewareOptions", mock.Anything).Return(mockClient)
				mockClient.On("GenerateContent", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockResponse, nil)
			}

			if tt.enabled && len(tt.entries) > 0 {
				mockStorage.On("UpdateConversationMetadata", mock.Anything, conversationID, mock.MatchedBy(func(meta storage.ConversationMetadata) bool {
					return meta.TitleGenerated == true && meta.TitleInvalidated == false && meta.Title == tt.expectedTitle
				})).Return(nil)
			}

			err := generator.GenerateTitleForConversation(context.Background(), conversationID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockStorage.AssertExpectations(t)
			if tt.enabled && len(tt.entries) > 0 && tt.aiResponse != "" {
				mockClient.AssertExpectations(t)
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

type FakeConversationStorage struct {
	mock.Mock
}

func (f *FakeConversationStorage) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata storage.ConversationMetadata) error {
	args := f.Called(ctx, conversationID, entries, metadata)
	return args.Error(0)
}

func (f *FakeConversationStorage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, storage.ConversationMetadata, error) {
	args := f.Called(ctx, conversationID)
	return args.Get(0).([]domain.ConversationEntry), args.Get(1).(storage.ConversationMetadata), args.Error(2)
}

func (f *FakeConversationStorage) ListConversations(ctx context.Context, limit, offset int) ([]storage.ConversationSummary, error) {
	args := f.Called(ctx, limit, offset)
	return args.Get(0).([]storage.ConversationSummary), args.Error(1)
}

func (f *FakeConversationStorage) ListConversationsNeedingTitles(ctx context.Context, limit int) ([]storage.ConversationSummary, error) {
	args := f.Called(ctx, limit)
	return args.Get(0).([]storage.ConversationSummary), args.Error(1)
}

func (f *FakeConversationStorage) DeleteConversation(ctx context.Context, conversationID string) error {
	args := f.Called(ctx, conversationID)
	return args.Error(0)
}

func (f *FakeConversationStorage) UpdateConversationMetadata(ctx context.Context, conversationID string, metadata storage.ConversationMetadata) error {
	args := f.Called(ctx, conversationID, metadata)
	return args.Error(0)
}

func (f *FakeConversationStorage) Close() error {
	args := f.Called()
	return args.Error(0)
}

func (f *FakeConversationStorage) Health(ctx context.Context) error {
	args := f.Called(ctx)
	return args.Error(0)
}

type FakeSDKClient struct {
	mock.Mock
}

func (f *FakeSDKClient) WithOptions(opts *sdk.CreateChatCompletionRequest) sdk.Client {
	f.Called(opts)
	return f
}

func (f *FakeSDKClient) WithMiddlewareOptions(opts *sdk.MiddlewareOptions) sdk.Client {
	f.Called(opts)
	return f
}

func (f *FakeSDKClient) GenerateContent(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error) {
	args := f.Called(ctx, provider, model, messages)
	return args.Get(0).(*sdk.CreateChatCompletionResponse), args.Error(1)
}

func (f *FakeSDKClient) GenerateContentStream(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (<-chan sdk.SSEvent, error) {
	args := f.Called(ctx, provider, model, messages)
	return args.Get(0).(<-chan sdk.SSEvent), args.Error(1)
}

func (f *FakeSDKClient) HealthCheck(ctx context.Context) error {
	args := f.Called(ctx)
	return args.Error(0)
}
