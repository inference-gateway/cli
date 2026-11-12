package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

func rewindEntry(role sdk.MessageRole, content string, hidden bool) convdomain.ConversationEntry {
	return convdomain.ConversationEntry{
		Message: sdk.Message{Role: role, Content: sdk.NewMessageContent(content)},
		Time:    time.Now(),
		Hidden:  hidden,
	}
}

// rewindConversation is two turns that ask for the same thing, with a hidden
// reminder of the same text between them.
func rewindConversation(t *testing.T) (*PersistentConversationRepository, storage.ConversationStorage) {
	t.Helper()
	store := storage.NewMemoryStorage()
	repo := NewPersistentConversationRepository(nil, nil, store)
	if err := repo.StartNewConversation("Rewind"); err != nil {
		t.Fatalf("StartNewConversation: %v", err)
	}
	entries := []convdomain.ConversationEntry{
		rewindEntry(sdk.User, "first", false),
		rewindEntry(sdk.Assistant, "first reply", false),
		rewindEntry(sdk.User, "again", false),
		rewindEntry(sdk.Assistant, "second reply", false),
		rewindEntry(sdk.User, "again", true),
		rewindEntry(sdk.User, "again", false),
		rewindEntry(sdk.Assistant, "again", false),
	}
	for _, entry := range entries {
		if err := repo.AddMessage(entry); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	return repo, store
}

func TestRewindToPromptDropsThePromptAndEverythingAfterIt(t *testing.T) {
	tests := []struct {
		name   string
		prompt convdomain.PromptRef
		kept   int
	}{
		{name: "the last time the prompt was sent", prompt: convdomain.PromptRef{Text: "again", Occurrence: 1}, kept: 5},
		{name: "no occurrence means the last time", prompt: convdomain.PromptRef{Text: "again"}, kept: 5},
		{name: "the time before, skipping the hidden entry", prompt: convdomain.PromptRef{Text: "again", Occurrence: 2}, kept: 2},
		{name: "the first prompt empties the conversation", prompt: convdomain.PromptRef{Text: "first", Occurrence: 1}, kept: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, store := rewindConversation(t)
			id := repo.GetCurrentConversationID()

			if err := repo.RewindToPrompt(tt.prompt); err != nil {
				t.Fatalf("RewindToPrompt: %v", err)
			}
			if got := len(repo.GetMessages()); got != tt.kept {
				t.Fatalf("kept %d entries, want %d", got, tt.kept)
			}
			if repo.GetCurrentConversationID() != id {
				t.Fatalf("the rewind moved to conversation %q, want it to stay on %q", repo.GetCurrentConversationID(), id)
			}
			stored, _, err := store.LoadConversation(context.Background(), id)
			if len(stored) != tt.kept {
				t.Fatalf("stored %d entries (%v), want %d", len(stored), err, tt.kept)
			}
		})
	}
}

func TestRewindToPromptLeavesTheConversationWithoutThePrompt(t *testing.T) {
	tests := []struct {
		name   string
		prompt convdomain.PromptRef
	}{
		{name: "the user never sent the text", prompt: convdomain.PromptRef{Text: "never sent"}},
		{name: "the text is only the assistant's", prompt: convdomain.PromptRef{Text: "first reply"}},
		{name: "the user sent it fewer times", prompt: convdomain.PromptRef{Text: "again", Occurrence: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, _ := rewindConversation(t)

			err := repo.RewindToPrompt(tt.prompt)
			if !errors.Is(err, convdomain.ErrPromptNotFound) {
				t.Fatalf("RewindToPrompt = %v, want ErrPromptNotFound", err)
			}
			if got := len(repo.GetMessages()); got != 7 {
				t.Fatalf("a refused rewind dropped entries, %d left", got)
			}
		})
	}
}
