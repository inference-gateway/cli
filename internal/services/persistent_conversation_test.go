package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/storage"
	sdk "github.com/inference-gateway/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRepository(t *testing.T) (*PersistentConversationRepository, func()) {
	tempDir, err := os.MkdirTemp("", "persistent_test_*")
	require.NoError(t, err)

	dbPath := filepath.Join(tempDir, "test.db")

	config := storage.SQLiteConfig{
		Path: dbPath,
	}

	storageBackend, err := storage.NewSQLiteStorage(config)
	require.NoError(t, err)

	formatterService := &ToolFormatterService{}
	repo := NewPersistentConversationRepository(formatterService, storageBackend)

	cleanup := func() {
		_ = repo.Close()
		_ = storageBackend.Close()
		_ = os.RemoveAll(tempDir)
	}

	return repo, cleanup
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
				Content: "Hello, test!",
			},
			Time:  time.Now(),
			Model: "claude-3",
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
				Content: "Hello! How can I help you?",
			},
			Time:  time.Now(),
			Model: "claude-3",
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
		assert.Equal(t, "Hello, test!", messages[0].Message.Content)
		assert.Equal(t, "Hello! How can I help you?", messages[1].Message.Content)
	})
}

func TestPersistentConversationRepository_ConversationManagement(t *testing.T) {
	repo, cleanup := setupTestRepository(t)
	defer cleanup()

	ctx := context.Background()

	err := repo.StartNewConversation("Test Conversation")
	require.NoError(t, err)

	entry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: "Initial message",
		},
		Time:  time.Now(),
		Model: "claude-3",
	}
	err = repo.AddMessage(entry)
	require.NoError(t, err)

	err = repo.SaveConversation(ctx)
	require.NoError(t, err)

	t.Run("List Saved Conversations", func(t *testing.T) {
		err := repo.StartNewConversation("Another Test")
		assert.NoError(t, err)

		entry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "Another message",
			},
			Time:  time.Now(),
			Model: "claude-3",
		}
		err = repo.AddMessage(entry)
		assert.NoError(t, err)

		err = repo.SaveConversation(ctx)
		assert.NoError(t, err)

		conversations, err := repo.ListSavedConversations(ctx, 10, 0)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(conversations), 2)

		titles := make([]string, len(conversations))
		for i, conv := range conversations {
			titles[i] = conv.Title
		}
		assert.Contains(t, titles, "Test Conversation")
		assert.Contains(t, titles, "Another Test")
	})

	t.Run("Update Conversation Title and Tags", func(t *testing.T) {
		originalTitle := repo.GetCurrentConversationMetadata().Title

		repo.SetConversationTitle("Updated Title")
		repo.SetConversationTags([]string{"test", "updated"})

		metadata := repo.GetCurrentConversationMetadata()
		assert.Equal(t, "Updated Title", metadata.Title)
		assert.Equal(t, []string{"test", "updated"}, metadata.Tags)

		err := repo.SaveConversation(ctx)
		assert.NoError(t, err)

		conversationID := repo.GetCurrentConversationID()

		err = repo.Clear()
		assert.NoError(t, err)

		err = repo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		metadata = repo.GetCurrentConversationMetadata()
		assert.Equal(t, "Updated Title", metadata.Title)
		assert.Equal(t, []string{"test", "updated"}, metadata.Tags)
		assert.NotEqual(t, originalTitle, metadata.Title)
	})

	t.Run("Delete Saved Conversation", func(t *testing.T) {
		conversationID := repo.GetCurrentConversationID()

		err := repo.DeleteSavedConversation(ctx, conversationID)
		assert.NoError(t, err)

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
		err := repo.AddTokenUsage(50, 75, 125)
		assert.NoError(t, err)

		stats := repo.GetSessionTokens()
		assert.Equal(t, 50, stats.TotalInputTokens)
		assert.Equal(t, 75, stats.TotalOutputTokens)
		assert.Equal(t, 125, stats.TotalTokens)
		assert.Equal(t, 1, stats.RequestCount)

		err = repo.AddTokenUsage(30, 45, 75)
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
				Content: "Auto save message",
			},
			Time:  time.Now(),
			Model: "claude-3",
		}

		err = repo.AddMessage(entry)
		assert.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		err = repo.Clear()
		assert.NoError(t, err)

		err = repo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		messages := repo.GetMessages()
		assert.Len(t, messages, 1)
		assert.Equal(t, "Auto save message", messages[0].Message.Content)
	})

	t.Run("Auto Start New Conversation", func(t *testing.T) {
		newRepo, cleanup := setupTestRepository(t)
		defer cleanup()

		newRepo.SetAutoSave(true)

		assert.Empty(t, newRepo.GetCurrentConversationID())

		entry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "First message that should auto-start conversation",
			},
			Time:  time.Now(),
			Model: "claude-3",
		}

		err := newRepo.AddMessage(entry)
		assert.NoError(t, err)

		conversationID := newRepo.GetCurrentConversationID()
		assert.NotEmpty(t, conversationID)

		metadata := newRepo.GetCurrentConversationMetadata()
		assert.Equal(t, "Auto-saved Conversation", metadata.Title)

		messageCount := newRepo.GetMessageCount()
		assert.Equal(t, 1, messageCount, "Message should be added to in-memory store")

		time.Sleep(200 * time.Millisecond)

		err = newRepo.Clear()
		assert.NoError(t, err)

		err = newRepo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		messages := newRepo.GetMessages()
		assert.Len(t, messages, 1)
		assert.Equal(t, "First message that should auto-start conversation", messages[0].Message.Content)
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
