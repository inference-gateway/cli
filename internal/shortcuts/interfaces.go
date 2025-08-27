package shortcuts

import (
	"context"
)

// Shortcut interface represents a chat shortcut that can be executed
type Shortcut interface {
	GetName() string
	GetDescription() string
	GetUsage() string
	Execute(ctx context.Context, args []string) (ShortcutResult, error)
	CanExecute(args []string) bool
}

// ShortcutResult represents the result of a shortcut execution
type ShortcutResult struct {
	Output     string
	Success    bool
	SideEffect SideEffectType
	Data       any
}

// SideEffectType defines the types of side effects a shortcut can have
type SideEffectType int

const (
	SideEffectNone SideEffectType = iota
	SideEffectClearConversation
	SideEffectExportConversation
	SideEffectExit
	SideEffectSwitchModel
	SideEffectShowHelp
	SideEffectReloadConfig
	SideEffectGenerateCommit
	SideEffectSaveConversation
	SideEffectShowConversationSelection
	SideEffectStartNewConversation
)

// PersistentConversationRepository interface for conversation persistence
type PersistentConversationRepository interface {
	ListSavedConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error)
	LoadConversation(ctx context.Context, conversationID string) error
	GetCurrentConversationMetadata() ConversationMetadata
	SaveConversation(ctx context.Context) error
	StartNewConversation(title string) error
	GetCurrentConversationID() string
	SetConversationTitle(title string)
	DeleteSavedConversation(ctx context.Context, conversationID string) error
}

// ConversationSummary represents a saved conversation summary
type ConversationSummary struct {
	ID           string
	Title        string
	CreatedAt    string
	UpdatedAt    string
	MessageCount int
	TokenStats   TokenStats
	Model        string
	Tags         []string
	Summary      string
}

// ConversationMetadata represents conversation metadata
type ConversationMetadata struct {
	ID           string
	Title        string
	CreatedAt    string
	UpdatedAt    string
	MessageCount int
	TokenStats   TokenStats
	Model        string
	Tags         []string
	Summary      string
}

// TokenStats represents token usage statistics
type TokenStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalTokens       int
	RequestCount      int
}
