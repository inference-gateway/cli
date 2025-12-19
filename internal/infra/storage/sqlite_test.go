package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func setupTestStorage(t *testing.T) (*SQLiteStorage, func()) {
	tempDir, err := os.MkdirTemp("", "sqlite_test_*")
	require.NoError(t, err)

	dbPath := filepath.Join(tempDir, "test.db")

	config := SQLiteConfig{
		Path: dbPath,
	}

	storage, err := NewSQLiteStorage(config)
	require.NoError(t, err)

	cleanup := func() {
		_ = storage.Close()
		_ = os.RemoveAll(tempDir)
	}

	return storage, cleanup
}

func TestSQLiteStorage_BasicOperations(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Health Check", func(t *testing.T) {
		err := storage.Health(ctx)
		assert.NoError(t, err)
	})

	t.Run("Save and Load Conversation", func(t *testing.T) {
		conversationID := "test-conversation-1"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		err := storage.SaveConversation(ctx, conversationID, entries, metadata)
		assert.NoError(t, err)

		loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		assert.Equal(t, metadata.ID, loadedMetadata.ID)
		assert.Equal(t, metadata.Title, loadedMetadata.Title)
		assert.Equal(t, len(entries), loadedMetadata.MessageCount)
		assert.Equal(t, metadata.TokenStats, loadedMetadata.TokenStats)
		assert.Equal(t, metadata.Tags, loadedMetadata.Tags)

		assert.Len(t, loadedEntries, len(entries))
		for i, entry := range entries {
			assert.Equal(t, entry.Message.Content, loadedEntries[i].Message.Content)
			assert.Equal(t, entry.Message.Role, loadedEntries[i].Message.Role)
			assert.Equal(t, entry.Model, loadedEntries[i].Model)
			assert.Equal(t, entry.Hidden, loadedEntries[i].Hidden)
		}
	})

	t.Run("Update Conversation", func(t *testing.T) {
		conversationID := "test-conversation-update"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		err := storage.SaveConversation(ctx, conversationID, entries, metadata)
		assert.NoError(t, err)

		newEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("Updated response"),
			},
			Time:  time.Now(),
			Model: "claude-4",
		}
		entries = append(entries, newEntry)

		metadata.Title = "Updated Title"
		metadata.UpdatedAt = time.Now()
		metadata.MessageCount = len(entries)

		err = storage.SaveConversation(ctx, conversationID, entries, metadata)
		assert.NoError(t, err)

		loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		assert.Equal(t, "Updated Title", loadedMetadata.Title)
		assert.Len(t, loadedEntries, len(entries))
		lastContent, _ := loadedEntries[len(loadedEntries)-1].Message.Content.AsMessageContent0()
		assert.Equal(t, "Updated response", lastContent)
	})
}

func TestSQLiteStorage_ConversationManagement(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("List Conversations", func(t *testing.T) {
		conversations := []string{"conv1", "conv2", "conv3"}

		for i, id := range conversations {
			entries := createTestEntries()
			metadata := createTestMetadata(id)
			metadata.Title = "Conversation " + string(rune('A'+i))
			metadata.CreatedAt = time.Now().Add(time.Duration(i) * time.Hour) // Different times
			metadata.UpdatedAt = metadata.CreatedAt

			err := storage.SaveConversation(ctx, id, entries, metadata)
			require.NoError(t, err)
		}

		summaries, err := storage.ListConversations(ctx, 10, 0)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(summaries), 3)

		for i := 1; i < len(summaries); i++ {
			assert.True(t, summaries[i-1].UpdatedAt.After(summaries[i].UpdatedAt) ||
				summaries[i-1].UpdatedAt.Equal(summaries[i].UpdatedAt))
		}
	})

	t.Run("Delete Conversation", func(t *testing.T) {
		conversationID := "test-conversation-delete"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		err := storage.SaveConversation(ctx, conversationID, entries, metadata)
		assert.NoError(t, err)

		_, _, err = storage.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		err = storage.DeleteConversation(ctx, conversationID)
		assert.NoError(t, err)

		_, _, err = storage.LoadConversation(ctx, conversationID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})

	t.Run("Update Metadata", func(t *testing.T) {
		conversationID := "test-conversation-metadata"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		err := storage.SaveConversation(ctx, conversationID, entries, metadata)
		assert.NoError(t, err)

		metadata.Title = "New Title"
		metadata.Tags = []string{"updated", "test"}
		metadata.Summary = "Updated summary"
		metadata.UpdatedAt = time.Now()

		err = storage.UpdateConversationMetadata(ctx, conversationID, metadata)
		assert.NoError(t, err)

		_, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		assert.NoError(t, err)

		assert.Equal(t, "New Title", loadedMetadata.Title)
		assert.Equal(t, []string{"updated", "test"}, loadedMetadata.Tags)
		assert.Equal(t, "Updated summary", loadedMetadata.Summary)
	})
}

func TestSQLiteStorage_ErrorCases(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Load Non-existent Conversation", func(t *testing.T) {
		_, _, err := storage.LoadConversation(ctx, "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})

	t.Run("Delete Non-existent Conversation", func(t *testing.T) {
		err := storage.DeleteConversation(ctx, "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})
}

func createTestEntries() []domain.ConversationEntry {
	now := time.Now()
	return []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Hello, world!"),
			},
			Time:  now,
			Model: "claude-4",
		},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("Hello! How can I help you today?"),
			},
			Time:  now.Add(time.Second),
			Model: "claude-4",
		},
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("What is the capital of France?"),
			},
			Time:  now.Add(2 * time.Second),
			Model: "claude-4",
		},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("The capital of France is Paris."),
			},
			Time:  now.Add(3 * time.Second),
			Model: "claude-4",
		},
	}
}

func createTestMetadata(id string) ConversationMetadata {
	now := time.Now()
	return ConversationMetadata{
		ID:        id,
		Title:     "Test Conversation",
		CreatedAt: now,
		UpdatedAt: now,
		TokenStats: domain.SessionTokenStats{
			TotalInputTokens:  100,
			TotalOutputTokens: 150,
			TotalTokens:       250,
			RequestCount:      2,
		},
		Model:   "claude-4",
		Tags:    []string{"test", "demo"},
		Summary: "A test conversation",
	}
}
