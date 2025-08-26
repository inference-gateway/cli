package shortcuts

import (
	"context"
)

// ConversationSelectShortcut shows the conversation selection dropdown
type ConversationSelectShortcut struct {
	repo PersistentConversationRepository
}

// NewConversationSelectShortcut creates a new conversation select shortcut
func NewConversationSelectShortcut(repo PersistentConversationRepository) *ConversationSelectShortcut {
	return &ConversationSelectShortcut{repo: repo}
}

func (c *ConversationSelectShortcut) GetName() string { return "conversations" }
func (c *ConversationSelectShortcut) GetDescription() string {
	return "Show conversation selection dropdown"
}
func (c *ConversationSelectShortcut) GetUsage() string              { return "/conversations" }
func (c *ConversationSelectShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ConversationSelectShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     "Opening conversation selection...",
		Success:    true,
		SideEffect: SideEffectShowConversationSelection,
	}, nil
}
