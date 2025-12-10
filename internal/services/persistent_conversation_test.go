package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	generated "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func setupTestRepository(t *testing.T) (*PersistentConversationRepository, func()) {
	config := storage.SQLiteConfig{
		Path: ":memory:",
	}

	storageBackend, err := storage.NewSQLiteStorage(config)
	require.NoError(t, err)

	formatterService := &ToolFormatterService{}
	repo := NewPersistentConversationRepository(formatterService, nil, storageBackend)

	cleanup := func() {
		_ = repo.Close()
		_ = storageBackend.Close()
	}

	return repo, cleanup
}

func TestGenerateTitleFromMessage(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty message",
			input:    "",
			expected: "New Conversation",
		},
		{
			name:     "Short message",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "10 word message",
			input:    "This is a test message with exactly ten words here",
			expected: "This is a test message with exactly ten words here",
		},
		{
			name:     "Long message gets truncated to 10 words",
			input:    "This is a very long message that contains more than ten words so it should be truncated",
			expected: "This is a very long message that contains more than",
		},
		{
			name:     "Message with extra spaces",
			input:    "   Hello    world   with    spaces   ",
			expected: "Hello world with spaces",
		},
		{
			name:     "Very long single word",
			input:    "ThisIsAVeryLongSingleWordThatExceedsTheCharacterLimitAndShouldBeTruncatedWithEllipsis1234567890",
			expected: "ThisIsAVeryLongSingleWordThatExceedsTheCharacterLimitAndShouldBeTruncatedWith...",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := domain.CreateTitleFromMessage(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestPersistentConversationRepository_BasicOperations(t *testing.T) {
	repo, cleanup := setupTestRepository(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Start New Conversation", func(t *testing.T) {
		err := repo.StartNewConversation("Test Conversation")
		assert.NoError(t, err)

		conversationID := repo.GetCurrentConversationID()
		assert.NotEmpty(t, conversationID)

		metadata := repo.GetCurrentConversationMetadata()
		assert.Equal(t, "Test Conversation", metadata.Title)
		assert.Equal(t, conversationID, metadata.ID)
		assert.Equal(t, 0, metadata.MessageCount)
	})

	t.Run("Add Messages and Auto Save", func(t *testing.T) {
		repo.SetAutoSave(false)

		entry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Hello, test!"),
			},
			Time:  time.Now(),
			Model: "claude-4",
		}

		err := repo.AddMessage(entry)
		assert.NoError(t, err)

		assert.Equal(t, 1, repo.GetMessageCount())
	})

	t.Run("Save and Load Conversation", func(t *testing.T) {
		conversationID := repo.GetCurrentConversationID()

		entry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("Hello! How can I help you?"),
			},
			Time:  time.Now(),
			Model: "claude-4",
		}

		err := repo.AddMessage(entry)
		assert.NoError(t, err)

		err = repo.SaveConversation(ctx)
		assert.NoError(t, err)

		err = repo.Clear()
		assert.NoError(t, err)
		assert.Equal(t, 0, repo.GetMessageCount())

		err = repo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		assert.Equal(t, 2, repo.GetMessageCount())
		assert.Equal(t, conversationID, repo.GetCurrentConversationID())

		messages := repo.GetMessages()
		assert.Len(t, messages, 2)
		loadedContent0, _ := messages[0].Message.Content.AsMessageContent0()
		assert.Equal(t, "Hello, test!", loadedContent0)
		loadedContent1, _ := messages[1].Message.Content.AsMessageContent0()
		assert.Equal(t, "Hello! How can I help you?", loadedContent1)
	})
}

func TestPersistentConversationRepository_AutoSaveTitle(t *testing.T) {
	repo, cleanup := setupTestRepository(t)
	defer cleanup()

	t.Run("Auto-save with user message creates title from content", func(t *testing.T) {
		repo.SetAutoSave(true)

		entry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("How do I implement a binary search tree in Go?"),
			},
			Time:  time.Now(),
			Model: "claude-4",
		}
		err := repo.AddMessage(entry)
		assert.NoError(t, err)

		time.Sleep(constants.TestSleepDelay)

		metadata := repo.GetCurrentConversationMetadata()
		assert.Equal(t, "How do I implement a binary search tree in Go?", metadata.Title)
	})
}

func TestPersistentConversationRepository_ConversationManagement(t *testing.T) {
	mockStorage := &generated.FakeConversationStorage{}
	formatterService := &ToolFormatterService{}
	repo := NewPersistentConversationRepository(formatterService, nil, mockStorage)

	ctx := context.Background()

	t.Run("List Saved Conversations", func(t *testing.T) {
		expectedConversations := []storage.ConversationSummary{
			{ID: "conv1", Title: "Test Conversation", MessageCount: 1},
			{ID: "conv2", Title: "Another Test", MessageCount: 1},
		}
		mockStorage.ListConversationsReturns(expectedConversations, nil)

		conversations, err := repo.ListSavedConversations(ctx, 10, 0)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(conversations), 2)

		titles := make([]string, len(conversations))
		for i, conv := range conversations {
			titles[i] = conv.Title
		}
		assert.Contains(t, titles, "Test Conversation")
		assert.Contains(t, titles, "Another Test")

		// Verify the mock was called with correct parameters
		assert.Equal(t, 1, mockStorage.ListConversationsCallCount())
		ctxArg, limitArg, offsetArg := mockStorage.ListConversationsArgsForCall(0)
		assert.Equal(t, ctx, ctxArg)
		assert.Equal(t, 10, limitArg)
		assert.Equal(t, 0, offsetArg)
	})

	t.Run("Update Conversation Title and Tags", func(t *testing.T) {
		conversationID := "test-conv-id"

		err := repo.StartNewConversation("Original Title")
		require.NoError(t, err)
		repo.conversationID = conversationID

		mockStorage.SaveConversationReturns(nil)

		repo.SetConversationTitle("Updated Title")
		repo.SetConversationTags([]string{"test", "updated"})

		metadata := repo.GetCurrentConversationMetadata()
		assert.Equal(t, "Updated Title", metadata.Title)
		assert.Equal(t, []string{"test", "updated"}, metadata.Tags)

		err = repo.SaveConversation(ctx)
		assert.NoError(t, err)

		assert.Equal(t, 1, mockStorage.SaveConversationCallCount())
		_, idArg, _, metadataArg := mockStorage.SaveConversationArgsForCall(0)
		assert.Equal(t, conversationID, idArg)
		assert.Equal(t, "Updated Title", metadataArg.Title)
		assert.Equal(t, []string{"test", "updated"}, metadataArg.Tags)
	})

	t.Run("Delete Saved Conversation", func(t *testing.T) {
		conversationID := "test-conv-id"

		mockStorage.DeleteConversationReturns(nil)
		mockStorage.LoadConversationReturns(nil, storage.ConversationMetadata{}, fmt.Errorf("conversation not found"))

		err := repo.DeleteSavedConversation(ctx, conversationID)
		assert.NoError(t, err)

		assert.Equal(t, 1, mockStorage.DeleteConversationCallCount())
		ctxArg, idArg := mockStorage.DeleteConversationArgsForCall(0)
		assert.Equal(t, ctx, ctxArg)
		assert.Equal(t, conversationID, idArg)

		err = repo.LoadConversation(ctx, conversationID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})
}

func TestPersistentConversationRepository_TokenTracking(t *testing.T) {
	repo, cleanup := setupTestRepository(t)
	defer cleanup()

	err := repo.StartNewConversation("Token Test")
	require.NoError(t, err)

	t.Run("Token Usage Tracking", func(t *testing.T) {
		err := repo.AddTokenUsage("test-model", 50, 75, 125)
		assert.NoError(t, err)

		stats := repo.GetSessionTokens()
		assert.Equal(t, 50, stats.TotalInputTokens)
		assert.Equal(t, 75, stats.TotalOutputTokens)
		assert.Equal(t, 125, stats.TotalTokens)
		assert.Equal(t, 1, stats.RequestCount)

		err = repo.AddTokenUsage("test-model", 30, 45, 75)
		assert.NoError(t, err)

		stats = repo.GetSessionTokens()
		assert.Equal(t, 80, stats.TotalInputTokens)
		assert.Equal(t, 120, stats.TotalOutputTokens)
		assert.Equal(t, 200, stats.TotalTokens)
		assert.Equal(t, 2, stats.RequestCount)

		metadata := repo.GetCurrentConversationMetadata()
		assert.Equal(t, stats, metadata.TokenStats)
	})
}

func TestPersistentConversationRepository_AutoSave(t *testing.T) {
	repo, cleanup := setupTestRepository(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Auto Save Functionality", func(t *testing.T) {
		repo.SetAutoSave(true)
		err := repo.StartNewConversation("Auto Save Test")
		assert.NoError(t, err)

		conversationID := repo.GetCurrentConversationID()

		entry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Auto save message"),
			},
			Time:  time.Now(),
			Model: "claude-4",
		}

		err = repo.AddMessage(entry)
		assert.NoError(t, err)

		time.Sleep(constants.TestSleepDelay)

		err = repo.Clear()
		assert.NoError(t, err)

		err = repo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		messages := repo.GetMessages()
		assert.Len(t, messages, 1)
		loadedContent, _ := messages[0].Message.Content.AsMessageContent0()
		assert.Equal(t, "Auto save message", loadedContent)
	})

	t.Run("Auto Start New Conversation", func(t *testing.T) {
		newRepo, cleanup := setupTestRepository(t)
		defer cleanup()

		newRepo.SetAutoSave(true)

		assert.Empty(t, newRepo.GetCurrentConversationID())

		entry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("First message that should auto-start conversation"),
			},
			Time:  time.Now(),
			Model: "claude-4",
		}

		err := newRepo.AddMessage(entry)
		assert.NoError(t, err)

		conversationID := newRepo.GetCurrentConversationID()
		assert.NotEmpty(t, conversationID)

		metadata := newRepo.GetCurrentConversationMetadata()
		assert.Equal(t, "First message that should auto-start conversation", metadata.Title)

		messageCount := newRepo.GetMessageCount()
		assert.Equal(t, 1, messageCount, "Message should be added to in-memory store")

		time.Sleep(constants.TestSleepDelay)

		err = newRepo.Clear()
		assert.NoError(t, err)

		err = newRepo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		messages := newRepo.GetMessages()
		assert.Len(t, messages, 1)
		loadedContent, _ := messages[0].Message.Content.AsMessageContent0()
		assert.Equal(t, "First message that should auto-start conversation", loadedContent)
	})
}

func TestPersistentConversationRepository_ErrorCases(t *testing.T) {
	repo, cleanup := setupTestRepository(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Load Non-existent Conversation", func(t *testing.T) {
		err := repo.LoadConversation(ctx, "non-existent-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load conversation")
	})

	t.Run("Save Without Active Conversation", func(t *testing.T) {
		err := repo.Clear()
		assert.NoError(t, err)

		err = repo.SaveConversation(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active conversation to save")
	})
}
