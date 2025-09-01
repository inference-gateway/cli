package keybinding

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// KeyHandler represents a function that handles a key binding
type KeyHandler func(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd

// KeyHandlerContext provides access to application context for key handlers
type KeyHandlerContext interface {
	// State management
	GetStateManager() *services.StateManager
	GetConversationRepository() domain.ConversationRepository
	GetConfig() *config.Config

	// Services
	GetAgentService() domain.AgentService

	// UI components
	GetConversationView() ui.ConversationRenderer
	GetInputView() ui.InputComponent
	GetStatusView() ui.StatusComponent

	// Actions
	ToggleToolResultExpansion()
	SendMessage() tea.Cmd
	GetPageSize() int
}

// Theme is an alias to the shared Theme interface
type Theme = shared.Theme

// KeyAction represents a key binding action with metadata
type KeyAction struct {
	ID          string
	Keys        []string
	Description string
	Category    string
	Handler     KeyHandler
	Context     KeyContext
	Priority    int
	Enabled     bool
}

// KeyContext defines when and where a key binding is active
type KeyContext struct {
	Views        []domain.ViewState
	Conditions   []ContextCondition
	ExcludeViews []domain.ViewState
}

// ContextCondition represents a condition that must be met for key binding to be active
type ContextCondition struct {
	Name  string
	Check func(app KeyHandlerContext) bool
}

// KeyLayer represents a layer of key bindings with specific priority
type KeyLayer struct {
	Name     string
	Priority int
	Bindings map[string]*KeyAction
	Matcher  LayerMatcher
}

// LayerMatcher determines if a layer is currently active
type LayerMatcher func(app KeyHandlerContext) bool

// KeyRegistry manages all key bindings and their resolution
type KeyRegistry interface {
	Register(action *KeyAction) error
	Unregister(id string) error
	Resolve(key string, app KeyHandlerContext) *KeyAction
	GetAction(id string) *KeyAction
	GetActiveActions(app KeyHandlerContext) []*KeyAction
	GetHelpShortcuts(app KeyHandlerContext) []HelpShortcut
	AddLayer(layer *KeyLayer)
	GetLayers() []*KeyLayer
}

// HelpShortcut represents a key shortcut for help display
type HelpShortcut struct {
	Key         string
	Description string
	Category    string
}
