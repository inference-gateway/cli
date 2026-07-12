package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

// runConversationStorageConformance runs the same behavioural suite against any
// ConversationStorage implementation. newStorage must return a fresh, isolated
// backend on every call so the groups below don't contaminate each other's
// list/count assertions. Backends that also implement SessionGroupStorage get
// the session-group group; the others skip it.
//
// This is the shared home for the assertions the sqlite and d1 tests used to
// copy by hand, and the first happy-path coverage the postgres backend has ever
// had (see issue #839).
func runConversationStorageConformance(t *testing.T, newStorage func(t *testing.T) ConversationStorage) {
	t.Helper()

	t.Run("BasicOperations", func(t *testing.T) {
		conformanceBasicOperations(t, newStorage(t))
	})
	t.Run("ConversationManagement", func(t *testing.T) {
		conformanceConversationManagement(t, newStorage(t))
	})
	t.Run("ErrorCases", func(t *testing.T) {
		conformanceErrorCases(t, newStorage(t))
	})
	t.Run("ListConversationsNeedingTitles", func(t *testing.T) {
		conformanceListNeedingTitles(t, newStorage(t))
	})
	t.Run("SessionGroups", func(t *testing.T) {
		groups, ok := newStorage(t).(SessionGroupStorage)
		if !ok {
			t.Skip("backend does not implement SessionGroupStorage")
		}
		conformanceSessionGroups(t, groups)
	})
}

func conformanceBasicOperations(t *testing.T, storage ConversationStorage) {
	ctx := context.Background()

	t.Run("Health Check", func(t *testing.T) {
		assert.NoError(t, storage.Health(ctx))
	})

	t.Run("Save and Load Conversation", func(t *testing.T) {
		conversationID := "test-conversation-1"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		require.NoError(t, err)

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

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

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

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		require.NoError(t, err)

		assert.Equal(t, "Updated Title", loadedMetadata.Title)
		assert.Len(t, loadedEntries, len(entries))
		lastContent, _ := loadedEntries[len(loadedEntries)-1].Message.Content.AsMessageContent0()
		assert.Equal(t, "Updated response", lastContent)
	})
}

func conformanceConversationManagement(t *testing.T, storage ConversationStorage) {
	ctx := context.Background()

	t.Run("List Conversations", func(t *testing.T) {
		conversations := []string{"conv1", "conv2", "conv3"}

		for i, id := range conversations {
			entries := createTestEntries()
			metadata := createTestMetadata(id)
			metadata.Title = "Conversation " + string(rune('A'+i))
			metadata.CreatedAt = time.Now().Add(time.Duration(i) * time.Hour)
			metadata.UpdatedAt = metadata.CreatedAt

			require.NoError(t, storage.SaveConversation(ctx, id, entries, metadata))
		}

		summaries, err := storage.ListConversations(ctx, 10, 0)
		require.NoError(t, err)
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

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		_, _, err := storage.LoadConversation(ctx, conversationID)
		require.NoError(t, err)

		require.NoError(t, storage.DeleteConversation(ctx, conversationID))

		_, _, err = storage.LoadConversation(ctx, conversationID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})

	t.Run("Update Metadata", func(t *testing.T) {
		conversationID := "test-conversation-metadata"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		metadata.Title = "New Title"
		metadata.Tags = []string{"updated", "test"}
		metadata.UpdatedAt = time.Now()

		require.NoError(t, storage.UpdateConversationMetadata(ctx, conversationID, metadata))

		_, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		require.NoError(t, err)

		assert.Equal(t, "New Title", loadedMetadata.Title)
		assert.Equal(t, []string{"updated", "test"}, loadedMetadata.Tags)
	})
}

func conformanceErrorCases(t *testing.T, storage ConversationStorage) {
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

// conformanceListNeedingTitles guards the title-generation batch path: it must
// receive the full mapper (Model, Tags, the real request_count) — not the lean
// ListConversations projection.
func conformanceListNeedingTitles(t *testing.T, storage ConversationStorage) {
	ctx := context.Background()

	for i := range 3 {
		id := fmt.Sprintf("needs-title-%d", i)
		entries := createTestEntries() // 4 entries → count >= 2
		metadata := createTestMetadata(id)
		metadata.TitleGenerated = false
		metadata.UpdatedAt = time.Now().Add(time.Duration(i) * time.Hour)
		require.NoError(t, storage.SaveConversation(ctx, id, entries, metadata))
	}

	summaries, err := storage.ListConversationsNeedingTitles(ctx, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(summaries), 3)

	for _, s := range summaries {
		assert.Equal(t, "claude-4", s.Model, "Model must be carried for the title batch path")
		assert.Equal(t, []string{"test", "demo"}, s.Tags, "Tags must be carried for the title batch path")
		assert.Equal(t, 2, s.TokenStats.RequestCount, "RequestCount must come from the stored column")
		assert.False(t, s.TitleGenerated)
	}
}

func conformanceSessionGroups(t *testing.T, s SessionGroupStorage) {
	ctx := context.Background()

	_, ok, err := s.GetSessionGroup(ctx, "missing")
	require.NoError(t, err)
	assert.False(t, ok, "missing key must report not-found")

	now := time.Now().UTC().Truncate(time.Second)
	entry := SessionGroupEntry{
		CurrentSessionID: "uuid-1",
		History:          []string{"prev-a", "prev-b"},
		LastRollover:     now,
		UpdatedAt:        now,
	}
	require.NoError(t, s.PutSessionGroup(ctx, "channel-telegram-42", entry))

	got, ok, err := s.GetSessionGroup(ctx, "channel-telegram-42")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "uuid-1", got.CurrentSessionID)
	assert.Equal(t, []string{"prev-a", "prev-b"}, got.History)
	assert.WithinDuration(t, now, got.UpdatedAt, time.Second)
	assert.WithinDuration(t, now, got.LastRollover, time.Second)

	require.NoError(t, s.PutSessionGroup(ctx, "channel-telegram-42", SessionGroupEntry{
		CurrentSessionID: "uuid-2",
		History:          []string{"prev-a", "prev-b", "uuid-1"},
		UpdatedAt:        now,
	}))

	got, ok, err = s.GetSessionGroup(ctx, "channel-telegram-42")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "uuid-2", got.CurrentSessionID)
	assert.Equal(t, []string{"prev-a", "prev-b", "uuid-1"}, got.History)
	assert.True(t, got.LastRollover.IsZero(), "LastRollover must be cleared when UPSERT supplies zero value")

	require.NoError(t, s.PutSessionGroup(ctx, "second", SessionGroupEntry{
		CurrentSessionID: "uuid-3",
		UpdatedAt:        now,
	}))
	all, err := s.ListSessionGroups(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)
	assert.Equal(t, "uuid-2", all["channel-telegram-42"].CurrentSessionID)
	assert.Equal(t, "uuid-3", all["second"].CurrentSessionID)
	assert.True(t, all["second"].LastRollover.IsZero())
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
		Model: "claude-4",
		Tags:  []string{"test", "demo"},
	}
}
