package conversation

import (
	"context"
	"time"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// RewindToPrompt drops the prompt and every entry after it, which puts the
// conversation back where the user was about to send that prompt. Unlike
// Clear it keeps the conversation's identity, also when no entry is left.
func (r *InMemoryConversationRepository) RewindToPrompt(prompt convdomain.PromptRef) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	index, found := promptEntryIndex(r.messages, prompt)
	if !found {
		return convdomain.ErrPromptNotFound
	}
	r.messages = r.messages[:index]
	return nil
}

// RewindToPrompt wraps the in-memory implementation with auto-save
func (r *PersistentConversationRepository) RewindToPrompt(prompt convdomain.PromptRef) error {
	if err := r.InMemoryConversationRepository.RewindToPrompt(prompt); err != nil {
		return err
	}
	return r.autoSaveShortened()
}

// autoSaveShortened stores a conversation that lost entries, when auto-save
// is on and the conversation has been stored before.
func (r *PersistentConversationRepository) autoSaveShortened() error {
	r.metadataMutex.RLock()
	shouldAutoSave := r.autoSave && r.conversationID != ""
	r.metadataMutex.RUnlock()
	if !shouldAutoSave {
		return nil
	}

	r.autoSaveMutex.Lock()
	defer r.autoSaveMutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := r.SaveConversation(ctx); err != nil {
		logger.Warn("failed to auto-save conversation after deleting messages", "error", err)
		return err
	}
	return nil
}

// promptEntryIndex finds the visible user entry a ref names, reading each
// entry's text the way a client is shown it.
func promptEntryIndex(entries []convdomain.ConversationEntry, prompt convdomain.PromptRef) (int, bool) {
	remaining := max(prompt.Occurrence, 1)
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.Hidden || entry.Message.Role != sdk.User {
			continue
		}
		if formatting.ExtractTextFromContent(entry.Message.Content, nil) != prompt.Text {
			continue
		}
		remaining--
		if remaining == 0 {
			return index, true
		}
	}
	return 0, false
}
