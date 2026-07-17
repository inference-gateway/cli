package storage

import (
	"context"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestMemoryStorage_SaveAndLoadConversation(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	conversationID := "test-conv-1"
	entries := []domain.ConversationEntry{
		{
			Message: domain.Message{
				Role:    domain.RoleUser,
				Content: sdk.NewMessageContent("Hello"),
			},
			Time: time.Now(),
		},
		{
			Message: domain.Message{
				Role:    domain.RoleAssistant,
				Content: sdk.NewMessageContent("Hi there!"),
			},
			Time: time.Now(),
		},
	}

	metadata := ConversationMetadata{
		ID:           conversationID,
		Title:        "Test Conversation",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		MessageCount: len(entries),
		TokenStats: domain.SessionTokenStats{
			TotalInputTokens:  10,
			TotalOutputTokens: 15,
			TotalTokens:       25,
			RequestCount:      1,
		},
	}

	err := storage.SaveConversation(ctx, conversationID, entries, metadata)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	if len(loadedEntries) != len(entries) {
		t.Errorf("Expected %d entries, got %d", len(entries), len(loadedEntries))
	}

	if loadedMetadata.ID != metadata.ID {
		t.Errorf("Expected ID %s, got %s", metadata.ID, loadedMetadata.ID)
	}

	if loadedMetadata.Title != metadata.Title {
		t.Errorf("Expected title %s, got %s", metadata.Title, loadedMetadata.Title)
	}
}

func TestMemoryStorage_ListConversations(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	conversations := []string{"conv-1", "conv-2", "conv-3"}

	for i, convID := range conversations {
		entries := []domain.ConversationEntry{
			{
				Message: domain.Message{
					Role:    domain.RoleUser,
					Content: sdk.NewMessageContent("Test message"),
				},
				Time: time.Now(),
			},
		}

		metadata := ConversationMetadata{
			ID:        convID,
			Title:     "Test Conversation " + convID,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Hour),
			UpdatedAt: time.Now().Add(time.Duration(i) * time.Hour),
		}

		err := storage.SaveConversation(ctx, convID, entries, metadata)
		if err != nil {
			t.Fatalf("SaveConversation failed for %s: %v", convID, err)
		}
	}

	summaries, err := storage.ListConversations(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}

	if len(summaries) != len(conversations) {
		t.Errorf("Expected %d summaries, got %d", len(conversations), len(summaries))
	}

	summaries, err = storage.ListConversations(ctx, 2, 0)
	if err != nil {
		t.Fatalf("ListConversations with limit failed: %v", err)
	}

	if len(summaries) != 2 {
		t.Errorf("Expected 2 summaries with limit, got %d", len(summaries))
	}
}

func TestMemoryStorage_DeleteConversation(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	conversationID := "test-conv-delete"
	entries := []domain.ConversationEntry{
		{
			Message: domain.Message{
				Role:    domain.RoleUser,
				Content: sdk.NewMessageContent("Hello"),
			},
			Time: time.Now(),
		},
	}

	metadata := ConversationMetadata{
		ID:        conversationID,
		Title:     "Test Conversation",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := storage.SaveConversation(ctx, conversationID, entries, metadata)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	err = storage.DeleteConversation(ctx, conversationID)
	if err != nil {
		t.Fatalf("DeleteConversation failed: %v", err)
	}

	_, _, err = storage.LoadConversation(ctx, conversationID)
	if err == nil {
		t.Error("Expected error when loading deleted conversation, but got nil")
	}
}

func TestMemoryStorage_UpdateMetadata(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	conversationID := "test-conv-update"
	entries := []domain.ConversationEntry{
		{
			Message: domain.Message{
				Role:    domain.RoleUser,
				Content: sdk.NewMessageContent("Hello"),
			},
			Time: time.Now(),
		},
	}

	metadata := ConversationMetadata{
		ID:        conversationID,
		Title:     "Original Title",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := storage.SaveConversation(ctx, conversationID, entries, metadata)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	updatedMetadata := metadata
	updatedMetadata.Title = "Updated Title"
	updatedMetadata.TitleGenerated = true

	err = storage.UpdateConversationMetadata(ctx, conversationID, updatedMetadata)
	if err != nil {
		t.Fatalf("UpdateConversationMetadata failed: %v", err)
	}

	_, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	if loadedMetadata.Title != "Updated Title" {
		t.Errorf("Expected title 'Updated Title', got %s", loadedMetadata.Title)
	}

	if !loadedMetadata.TitleGenerated {
		t.Error("Expected TitleGenerated to be true")
	}
}

func TestMemoryStorage_Health(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	err := storage.Health(ctx)
	if err != nil {
		t.Errorf("Expected Health to return nil, got %v", err)
	}
}

func TestMemoryStorage_Close(t *testing.T) {
	storage := NewMemoryStorage()

	conversationID := "test-conv-close"
	entries := []domain.ConversationEntry{
		{
			Message: domain.Message{
				Role:    domain.RoleUser,
				Content: sdk.NewMessageContent("Hello"),
			},
			Time: time.Now(),
		},
	}

	metadata := ConversationMetadata{
		ID:        conversationID,
		Title:     "Test Conversation",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := storage.SaveConversation(context.Background(), conversationID, entries, metadata)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	err = storage.Close()
	if err != nil {
		t.Errorf("Expected Close to return nil, got %v", err)
	}

	_, _, err = storage.LoadConversation(context.Background(), conversationID)
	if err == nil {
		t.Error("Expected error when loading conversation after Close, but got nil")
	}
}

func TestMemoryStorage_SessionGroups(t *testing.T) {
	s := NewMemoryStorage()
	ctx := context.Background()

	if _, ok, err := s.GetSessionGroup(ctx, "missing"); err != nil || ok {
		t.Fatalf("missing group should be (_, false, nil); got ok=%v err=%v", ok, err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	entry := SessionGroupEntry{
		CurrentSessionID: "id-1",
		History:          []string{"prev-1", "prev-2"},
		LastRollover:     now,
		UpdatedAt:        now,
	}
	if err := s.PutSessionGroup(ctx, "channel-test", entry); err != nil {
		t.Fatalf("PutSessionGroup: %v", err)
	}

	got, ok, err := s.GetSessionGroup(ctx, "channel-test")
	if err != nil || !ok {
		t.Fatalf("expected entry; ok=%v err=%v", ok, err)
	}
	if got.CurrentSessionID != "id-1" {
		t.Errorf("CurrentSessionID: got %q want id-1", got.CurrentSessionID)
	}
	if len(got.History) != 2 || got.History[0] != "prev-1" || got.History[1] != "prev-2" {
		t.Errorf("History: got %v", got.History)
	}

	got.History[0] = "tampered"
	again, _, _ := s.GetSessionGroup(ctx, "channel-test")
	if again.History[0] != "prev-1" {
		t.Errorf("entry must be cloned on read: got %q", again.History[0])
	}

	if err := s.PutSessionGroup(ctx, "channel-test", SessionGroupEntry{
		CurrentSessionID: "id-2", UpdatedAt: now,
	}); err != nil {
		t.Fatalf("overwrite Put: %v", err)
	}
	got, _, _ = s.GetSessionGroup(ctx, "channel-test")
	if got.CurrentSessionID != "id-2" {
		t.Errorf("overwrite failed: got %q", got.CurrentSessionID)
	}

	if err := s.PutSessionGroup(ctx, "second", SessionGroupEntry{
		CurrentSessionID: "id-3", UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Put second: %v", err)
	}
	all, err := s.ListSessionGroups(ctx)
	if err != nil {
		t.Fatalf("ListSessionGroups: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List should return 2, got %d", len(all))
	}
}

func TestNewMemorySessionGroupStorage_StandaloneFallback(t *testing.T) {
	s := NewMemorySessionGroupStorage()
	ctx := context.Background()

	if err := s.PutSessionGroup(ctx, "g", SessionGroupEntry{CurrentSessionID: "x", UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if entry, ok, err := s.GetSessionGroup(ctx, "g"); err != nil || !ok || entry.CurrentSessionID != "x" {
		t.Errorf("standalone constructor must return a working store; ok=%v err=%v entry=%+v", ok, err, entry)
	}
}

// TestMemoryStorage_Conformance runs the shared behavioural suites for the
// scheduled-job, plan, and shell-history stores against the memory backend.
func TestMemoryStorage_Conformance(t *testing.T) {
	runScheduledJobStorageConformance(t, func(t *testing.T) ScheduledJobStorage {
		return NewMemoryStorage()
	})
	runPlanStorageConformance(t, func(t *testing.T) PlanStorage {
		return NewMemoryStorage()
	})
	runShellHistoryStorageConformance(t, func(t *testing.T) ShellHistoryStorage {
		return NewMemoryStorage()
	})
}
