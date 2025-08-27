package storage

import (
	"context"
	"testing"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
)

func TestMemoryStorage_SaveAndLoadConversation(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	conversationID := "test-conv-1"
	entries := []domain.ConversationEntry{
		{
			Message: domain.Message{
				Role:    domain.RoleUser,
				Content: "Hello",
			},
			Time: time.Now(),
		},
		{
			Message: domain.Message{
				Role:    domain.RoleAssistant,
				Content: "Hi there!",
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
					Content: "Test message",
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
				Content: "Hello",
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
				Content: "Hello",
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

func TestMemoryStorage_ListConversationsNeedingTitles(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	conversations := []struct {
		id               string
		titleGenerated   bool
		titleInvalidated bool
	}{
		{"conv-1", false, false}, // needs title
		{"conv-2", true, false},  // has title
		{"conv-3", true, true},   // title invalidated, needs new title
		{"conv-4", false, false}, // needs title
	}

	for _, conv := range conversations {
		entries := []domain.ConversationEntry{
			{
				Message: domain.Message{
					Role:    domain.RoleUser,
					Content: "Test message",
				},
				Time: time.Now(),
			},
		}

		metadata := ConversationMetadata{
			ID:               conv.id,
			Title:            "Test Conversation " + conv.id,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
			TitleGenerated:   conv.titleGenerated,
			TitleInvalidated: conv.titleInvalidated,
		}

		err := storage.SaveConversation(ctx, conv.id, entries, metadata)
		if err != nil {
			t.Fatalf("SaveConversation failed for %s: %v", conv.id, err)
		}
	}

	summaries, err := storage.ListConversationsNeedingTitles(ctx, 0)
	if err != nil {
		t.Fatalf("ListConversationsNeedingTitles failed: %v", err)
	}

	expectedCount := 3 // conv-1, conv-3, conv-4
	if len(summaries) != expectedCount {
		t.Errorf("Expected %d conversations needing titles, got %d", expectedCount, len(summaries))
	}

	summaries, err = storage.ListConversationsNeedingTitles(ctx, 2)
	if err != nil {
		t.Fatalf("ListConversationsNeedingTitles with limit failed: %v", err)
	}

	if len(summaries) != 2 {
		t.Errorf("Expected 2 conversations with limit, got %d", len(summaries))
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
				Content: "Hello",
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
