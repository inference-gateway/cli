package shortcuts

import (
	"context"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
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
	SideEffectExit
	SideEffectSwitchModel
	SideEffectSwitchTheme
	SideEffectShowHelp
	SideEffectReloadConfig
	SideEffectSaveConversation
	SideEffectShowConversationSelection
	SideEffectStartNewConversation
	SideEffectShowA2ATaskManagement
	SideEffectSetInput
	SideEffectGenerateSnippet
	SideEffectCompactConversation
	SideEffectShowInstallOpentaskSetup
	SideEffectEmbedImages
	SideEffectSendMessageWithModel
	// SideEffectSendMessage submits Data (a string) as a regular chat message.
	SideEffectSendMessage
	SideEffectShowDiffViewer
	SideEffectShowExplorer
	SideEffectShowToolsList
	SideEffectShowA2AAgents
)

// PersistentConversationRepository interface for conversation persistence
type PersistentConversationRepository interface {
	ListSavedConversations(ctx context.Context, limit, offset int) ([]convdomain.ConversationSummary, error)
	LoadConversation(ctx context.Context, conversationID string) error
	GetCurrentConversationMetadata() convdomain.ConversationMetadata
	SaveConversation(ctx context.Context) error
	StartNewConversation(title string) error
	GetCurrentConversationID() string
	SetConversationTitle(title string)
	DeleteSavedConversation(ctx context.Context, conversationID string) error
}
