package shortcuts

import (
	"context"

	config "github.com/inference-gateway/cli/config"
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
	SideEffectShowInitGithubActionSetup
	SideEffectEmbedImages
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

// AgentsConfigService interface for managing agent configurations
type AgentsConfigService interface {
	AddAgent(agent config.AgentEntry) error
	UpdateAgent(agent config.AgentEntry) error
	RemoveAgent(name string) error
	ListAgents() ([]config.AgentEntry, error)
	GetAgent(name string) (*config.AgentEntry, error)
	GetAgentURLs() ([]string, error)
}
