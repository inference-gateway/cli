package keybinding

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	ui "github.com/inference-gateway/cli/internal/ui"
)

// KeyHandler represents a function that handles a key binding
type KeyHandler func(app KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd

// KeyHandlerContext provides access to application context for key handlers
type KeyHandlerContext interface {
	// State management
	GetStateManager() *services.StateManager
	GetConversationRepository() domain.ConversationRepository
	GetConfig() *config.Config
	GetConfigDir() string

	// Services
	GetAgentService() domain.AgentService
	GetImageService() domain.ImageService

	// UI components
	GetConversationView() ui.ConversationRenderer
	GetInputView() ui.InputComponent
	GetStatusView() ui.StatusComponent
	GetAutocomplete() ui.AutocompleteComponent

	// Actions
	ToggleToolResultExpansion()
	ToggleThinkingExpansion()
	ToggleRawFormat()
	SendMessage() tea.Cmd
	GetPageSize() int

	// Mouse mode
	GetMouseEnabled() bool
	SetMouseEnabled(bool)
}

// Theme is an alias to the ui Theme interface
type Theme = ui.Theme

// KeyAction represents a key binding action. Keys, description, and enabled
// state live in the Binding, which is constructed at registry init from the
// resolved keybindings config (defaults + keybindings.yaml overrides) — the
// single source of truth shared by dispatch and help rendering.
type KeyAction struct {
	ID       string
	Category string
	Binding  key.Binding
	Handler  KeyHandler
	Context  KeyContext
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

// HelpShortcut represents a key shortcut for help display
type HelpShortcut struct {
	Key         string
	Description string
	Category    string
}
