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

func TestPersistentConversationRepository(t *testing.T) {
	// Create temp directory for test database
	tempDir, err := os.MkdirTemp("", "persistent_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")

	config := storage.SQLiteConfig{
		Path: dbPath,
	}

	storageBackend, err := storage.NewSQLiteStorage(config)
	require.NoError(t, err)
	defer storageBackend.Close()

	// Create repository with mock formatter service
	formatterService := &ToolFormatterService{} // Using empty service for test
	repo := NewPersistentConversationRepository(formatterService, storageBackend)
	defer repo.Close()

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
		// Disable auto-save for controlled testing
		repo.SetAutoSave(false)

		// Add a message
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

		// Add another message
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

		// Save conversation
		err = repo.SaveConversation(ctx)
		assert.NoError(t, err)

		// Clear current conversation and load it back
		err = repo.Clear()
		assert.NoError(t, err)
		assert.Equal(t, 0, repo.GetMessageCount())

		// Load conversation
		err = repo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		assert.Equal(t, 2, repo.GetMessageCount())
		assert.Equal(t, conversationID, repo.GetCurrentConversationID())

		messages := repo.GetMessages()
		assert.Len(t, messages, 2)
		assert.Equal(t, "Hello, test!", messages[0].Message.Content)
		assert.Equal(t, "Hello! How can I help you?", messages[1].Message.Content)
	})

	t.Run("List Saved Conversations", func(t *testing.T) {
		// Create another conversation
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

		// List conversations
		conversations, err := repo.ListSavedConversations(ctx, 10, 0)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(conversations), 2)

		// Find our conversations
		titles := make([]string, len(conversations))
		for i, conv := range conversations {
			titles[i] = conv.Title
		}
		assert.Contains(t, titles, "Test Conversation")
		assert.Contains(t, titles, "Another Test")
	})

	t.Run("Update Conversation Title and Tags", func(t *testing.T) {
		originalTitle := repo.GetCurrentConversationMetadata().Title

		// Update title and tags
		repo.SetConversationTitle("Updated Title")
		repo.SetConversationTags([]string{"test", "updated"})

		metadata := repo.GetCurrentConversationMetadata()
		assert.Equal(t, "Updated Title", metadata.Title)
		assert.Equal(t, []string{"test", "updated"}, metadata.Tags)

		// Save and verify persistence
		err := repo.SaveConversation(ctx)
		assert.NoError(t, err)

		conversationID := repo.GetCurrentConversationID()

		// Clear and reload
		err = repo.Clear()
		assert.NoError(t, err)

		err = repo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		metadata = repo.GetCurrentConversationMetadata()
		assert.Equal(t, "Updated Title", metadata.Title)
		assert.Equal(t, []string{"test", "updated"}, metadata.Tags)
		assert.NotEqual(t, originalTitle, metadata.Title)
	})

	t.Run("Token Usage Tracking", func(t *testing.T) {
		// Add token usage
		err := repo.AddTokenUsage(50, 75, 125)
		assert.NoError(t, err)

		stats := repo.GetSessionTokens()
		assert.Equal(t, 50, stats.TotalInputTokens)
		assert.Equal(t, 75, stats.TotalOutputTokens)
		assert.Equal(t, 125, stats.TotalTokens)
		assert.Equal(t, 1, stats.RequestCount)

		// Add more usage
		err = repo.AddTokenUsage(30, 45, 75)
		assert.NoError(t, err)

		stats = repo.GetSessionTokens()
		assert.Equal(t, 80, stats.TotalInputTokens)
		assert.Equal(t, 120, stats.TotalOutputTokens)
		assert.Equal(t, 200, stats.TotalTokens)
		assert.Equal(t, 2, stats.RequestCount)

		// Verify metadata reflects the stats
		metadata := repo.GetCurrentConversationMetadata()
		assert.Equal(t, stats, metadata.TokenStats)
	})

	t.Run("Delete Saved Conversation", func(t *testing.T) {
		conversationID := repo.GetCurrentConversationID()

		// Delete conversation
		err := repo.DeleteSavedConversation(ctx, conversationID)
		assert.NoError(t, err)

		// Try to load deleted conversation
		err = repo.LoadConversation(ctx, conversationID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})

	t.Run("Auto Save Functionality", func(t *testing.T) {
		// Start new conversation with auto-save enabled
		repo.SetAutoSave(true)
		err := repo.StartNewConversation("Auto Save Test")
		assert.NoError(t, err)

		conversationID := repo.GetCurrentConversationID()

		// Add a message (should trigger auto-save)
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

		// Give auto-save goroutine time to complete
		time.Sleep(100 * time.Millisecond)

		// Clear and reload to verify auto-save worked
		err = repo.Clear()
		assert.NoError(t, err)

		err = repo.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		messages := repo.GetMessages()
		assert.Len(t, messages, 1)
		assert.Equal(t, "Auto save message", messages[0].Message.Content)
	})

	t.Run("Load Non-existent Conversation", func(t *testing.T) {
		err := repo.LoadConversation(ctx, "non-existent-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load conversation")
	})

	t.Run("Save Without Active Conversation", func(t *testing.T) {
		// Clear conversation
		err := repo.Clear()
		assert.NoError(t, err)

		// Try to save without active conversation
		err = repo.SaveConversation(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active conversation to save")
	})
}
