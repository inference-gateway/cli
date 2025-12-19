package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func setupTestJsonlStorage(t *testing.T) (*JsonlStorage, string, func()) {
	tempDir, err := os.MkdirTemp("", "jsonl_test_*")
	require.NoError(t, err)

	storage, err := NewJsonlStorage(JsonlStorageConfig{Path: tempDir})
	require.NoError(t, err)

	cleanup := func() {
		_ = storage.Close()
		_ = os.RemoveAll(tempDir)
	}

	return storage, tempDir, cleanup
}

func TestNewJsonlStorage(t *testing.T) {
	t.Run("creates directory if not exists", func(t *testing.T) {
		tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("jsonl_test_new_%d", time.Now().UnixNano()))
		defer func() { _ = os.RemoveAll(tempDir) }()

		storage, err := NewJsonlStorage(JsonlStorageConfig{Path: tempDir})
		require.NoError(t, err)
		require.NotNil(t, storage)
		defer func() { _ = storage.Close() }()

		info, err := os.Stat(tempDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("handles existing directory", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "jsonl_test_existing_*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()

		storage, err := NewJsonlStorage(JsonlStorageConfig{Path: tempDir})
		require.NoError(t, err)
		require.NotNil(t, storage)
		defer func() { _ = storage.Close() }()
	})

	t.Run("expands tilde in path", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		require.NoError(t, err)

		testPath := filepath.Join(homeDir, fmt.Sprintf(".jsonl_test_%d", time.Now().UnixNano()))
		defer func() { _ = os.RemoveAll(testPath) }()

		tildeConfig := JsonlStorageConfig{Path: "~/" + filepath.Base(testPath)}
		storage, err := NewJsonlStorage(tildeConfig)
		require.NoError(t, err)
		require.NotNil(t, storage)
		defer func() { _ = storage.Close() }()

		_, err = os.Stat(testPath)
		require.NoError(t, err)
	})
}

func TestJsonlStorage_SaveAndLoad(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()
	conversationID := "test-conversation-1"

	entries := []domain.ConversationEntry{
		{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Hello")},
			Model:   "claude-3",
			Time:    time.Now(),
			Hidden:  false,
		},
		{
			Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("Hi there!")},
			Model:   "claude-3",
			Time:    time.Now(),
			Hidden:  false,
		},
	}

	metadata := ConversationMetadata{
		ID:           conversationID,
		Title:        "Test Conversation",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		MessageCount: 2,
		TokenStats: domain.SessionTokenStats{
			TotalInputTokens:  10,
			TotalOutputTokens: 20,
			TotalTokens:       30,
			RequestCount:      1,
		},
	}

	err := storage.SaveConversation(ctx, conversationID, entries, metadata)
	require.NoError(t, err)

	loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
	require.NoError(t, err)

	assert.Equal(t, len(entries), len(loadedEntries))
	assert.Equal(t, metadata.ID, loadedMetadata.ID)
	assert.Equal(t, metadata.Title, loadedMetadata.Title)
	assert.Equal(t, metadata.MessageCount, loadedMetadata.MessageCount)
	assert.Equal(t, metadata.TokenStats.TotalInputTokens, loadedMetadata.TokenStats.TotalInputTokens)
	assert.Equal(t, metadata.TokenStats.TotalOutputTokens, loadedMetadata.TokenStats.TotalOutputTokens)

	assert.Equal(t, entries[0].Message.Role, loadedEntries[0].Message.Role)
	assert.Equal(t, entries[0].Message.Content, loadedEntries[0].Message.Content)
	assert.Equal(t, entries[1].Message.Role, loadedEntries[1].Message.Role)
	assert.Equal(t, entries[1].Message.Content, loadedEntries[1].Message.Content)
}

func TestJsonlStorage_LoadNonExistent(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()

	_, _, err := storage.LoadConversation(ctx, "non-existent-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conversation not found")
}

func TestJsonlStorage_List(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		conversationID := fmt.Sprintf("conversation-%d", i)
		entries := []domain.ConversationEntry{}
		metadata := ConversationMetadata{
			ID:           conversationID,
			Title:        fmt.Sprintf("Conversation %d", i),
			CreatedAt:    time.Now().Add(time.Duration(-i) * time.Hour),
			UpdatedAt:    time.Now().Add(time.Duration(i) * time.Minute),
			MessageCount: i + 1,
		}
		err := storage.SaveConversation(ctx, conversationID, entries, metadata)
		require.NoError(t, err)
	}

	t.Run("list all conversations", func(t *testing.T) {
		summaries, err := storage.ListConversations(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 5, len(summaries))

		for i := 0; i < len(summaries)-1; i++ {
			assert.True(t, summaries[i].UpdatedAt.After(summaries[i+1].UpdatedAt) || summaries[i].UpdatedAt.Equal(summaries[i+1].UpdatedAt))
		}
	})

	t.Run("pagination with limit", func(t *testing.T) {
		summaries, err := storage.ListConversations(ctx, 2, 0)
		require.NoError(t, err)
		assert.Equal(t, 2, len(summaries))
	})

	t.Run("pagination with offset", func(t *testing.T) {
		summaries, err := storage.ListConversations(ctx, 0, 3)
		require.NoError(t, err)
		assert.Equal(t, 2, len(summaries))
	})

	t.Run("pagination with limit and offset", func(t *testing.T) {
		summaries, err := storage.ListConversations(ctx, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, 2, len(summaries))
	})

	t.Run("offset beyond available conversations", func(t *testing.T) {
		summaries, err := storage.ListConversations(ctx, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, 0, len(summaries))
	})
}

func TestJsonlStorage_ListConversationsNeedingTitles(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()

	testCases := []struct {
		id               string
		messageCount     int
		titleGenerated   bool
		titleInvalidated bool
		shouldNeedTitle  bool
	}{
		{"conv-1", 5, false, false, true},
		{"conv-2", 1, false, false, false},
		{"conv-3", 5, true, false, false},
		{"conv-4", 5, true, true, true},
		{"conv-5", 3, false, false, true},
	}

	for _, tc := range testCases {
		metadata := ConversationMetadata{
			ID:               tc.id,
			Title:            "Test",
			MessageCount:     tc.messageCount,
			TitleGenerated:   tc.titleGenerated,
			TitleInvalidated: tc.titleInvalidated,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		err := storage.SaveConversation(ctx, tc.id, []domain.ConversationEntry{}, metadata)
		require.NoError(t, err)
	}

	summaries, err := storage.ListConversationsNeedingTitles(ctx, 0)
	require.NoError(t, err)

	expectedCount := 0
	for _, tc := range testCases {
		if tc.shouldNeedTitle {
			expectedCount++
		}
	}

	assert.Equal(t, expectedCount, len(summaries))

	needingTitleIDs := make(map[string]bool)
	for _, summary := range summaries {
		needingTitleIDs[summary.ID] = true
	}

	for _, tc := range testCases {
		if tc.shouldNeedTitle {
			assert.True(t, needingTitleIDs[tc.id], "Expected %s to need title", tc.id)
		} else {
			assert.False(t, needingTitleIDs[tc.id], "Expected %s to not need title", tc.id)
		}
	}
}

func TestJsonlStorage_ListConversationsNeedingTitles_Limit(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		metadata := ConversationMetadata{
			ID:             fmt.Sprintf("conv-%d", i),
			Title:          "Test",
			MessageCount:   5,
			TitleGenerated: false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		err := storage.SaveConversation(ctx, metadata.ID, []domain.ConversationEntry{}, metadata)
		require.NoError(t, err)
	}

	summaries, err := storage.ListConversationsNeedingTitles(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, len(summaries))
}

func TestJsonlStorage_Delete(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()
	conversationID := "test-delete"

	err := storage.SaveConversation(ctx, conversationID, []domain.ConversationEntry{}, ConversationMetadata{
		ID:        conversationID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	require.NoError(t, err)

	_, _, err = storage.LoadConversation(ctx, conversationID)
	require.NoError(t, err)

	err = storage.DeleteConversation(ctx, conversationID)
	require.NoError(t, err)

	_, _, err = storage.LoadConversation(ctx, conversationID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conversation not found")
}

func TestJsonlStorage_DeleteNonExistent(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()

	err := storage.DeleteConversation(ctx, "non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conversation not found")
}

func TestJsonlStorage_UpdateMetadata(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()
	conversationID := "test-update-metadata"

	entries := []domain.ConversationEntry{
		{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Hello")},
			Model:   "claude-3",
			Time:    time.Now(),
		},
	}
	metadata := ConversationMetadata{
		ID:             conversationID,
		Title:          "Original Title",
		MessageCount:   1,
		TitleGenerated: false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	err := storage.SaveConversation(ctx, conversationID, entries, metadata)
	require.NoError(t, err)

	updatedMetadata := metadata
	updatedMetadata.Title = "Updated Title"
	updatedMetadata.TitleGenerated = true
	updatedMetadata.MessageCount = 2
	err = storage.UpdateConversationMetadata(ctx, conversationID, updatedMetadata)
	require.NoError(t, err)

	loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
	require.NoError(t, err)

	assert.Equal(t, "Updated Title", loadedMetadata.Title)
	assert.True(t, loadedMetadata.TitleGenerated)
	assert.Equal(t, 2, loadedMetadata.MessageCount)

	assert.Equal(t, len(entries), len(loadedEntries))
	assert.Equal(t, entries[0].Message.Content, loadedEntries[0].Message.Content)
}

func TestJsonlStorage_UpdateMetadataNonExistent(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()

	metadata := ConversationMetadata{
		ID:        "non-existent",
		Title:     "Test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := storage.UpdateConversationMetadata(ctx, "non-existent", metadata)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conversation not found")
}

func TestJsonlStorage_Health(t *testing.T) {
	storage, tempDir, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("healthy storage", func(t *testing.T) {
		err := storage.Health(ctx)
		require.NoError(t, err)
	})

	t.Run("directory removed", func(t *testing.T) {
		_ = os.RemoveAll(tempDir)

		err := storage.Health(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not accessible")
	})
}

func TestJsonlStorage_Close(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	err := storage.Close()
	require.NoError(t, err)
}

func TestJsonlStorage_ConcurrentAccess(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("concurrent writes", func(t *testing.T) {
		done := make(chan bool, 3)

		for i := 0; i < 3; i++ {
			go func(id int) {
				conversationID := fmt.Sprintf("concurrent-%d", id)
				metadata := ConversationMetadata{
					ID:        conversationID,
					Title:     fmt.Sprintf("Concurrent %d", id),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				err := storage.SaveConversation(ctx, conversationID, []domain.ConversationEntry{}, metadata)
				assert.NoError(t, err)
				done <- true
			}(i)
		}

		for i := 0; i < 3; i++ {
			<-done
		}

		summaries, err := storage.ListConversations(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 3, len(summaries))
	})
}

func TestJsonlStorage_LargeConversation(t *testing.T) {
	storage, _, cleanup := setupTestJsonlStorage(t)
	defer cleanup()

	ctx := context.Background()
	conversationID := "large-conversation"

	entries := make([]domain.ConversationEntry, 1000)
	for i := 0; i < 1000; i++ {
		entries[i] = domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent(fmt.Sprintf("Message %d with some content to make it realistic", i)),
			},
			Model: "claude-3",
			Time:  time.Now(),
		}
	}

	metadata := ConversationMetadata{
		ID:           conversationID,
		Title:        "Large Conversation",
		MessageCount: 1000,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err := storage.SaveConversation(ctx, conversationID, entries, metadata)
	require.NoError(t, err)

	loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
	require.NoError(t, err)

	assert.Equal(t, 1000, len(loadedEntries))
	assert.Equal(t, metadata.MessageCount, loadedMetadata.MessageCount)
}
