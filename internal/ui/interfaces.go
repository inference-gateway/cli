package ui

import (
	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// Re-export shared types for backward compatibility
type KeyShortcut = shared.KeyShortcut

// Simple theme for backward compatibility with autocomplete/model_selection
type Theme interface {
	GetUserColor() string
	GetAssistantColor() string
	GetErrorColor() string
	GetStatusColor() string
	GetAccentColor() string
	GetDimColor() string
	GetBorderColor() string
}

type DefaultTheme struct{}

func NewDefaultTheme() *DefaultTheme { return &DefaultTheme{} }

func (t *DefaultTheme) GetUserColor() string      { return "\033[36m" } // Cyan
func (t *DefaultTheme) GetAssistantColor() string { return "\033[32m" } // Green
func (t *DefaultTheme) GetErrorColor() string     { return "\033[31m" } // Red
func (t *DefaultTheme) GetStatusColor() string    { return "\033[34m" } // Blue
func (t *DefaultTheme) GetAccentColor() string    { return "\033[35m" } // Magenta
func (t *DefaultTheme) GetDimColor() string       { return "\033[90m" } // Gray
func (t *DefaultTheme) GetBorderColor() string    { return "\033[37m" } // White

// ConversationRenderer interface for rendering conversation history
type ConversationRenderer interface {
	SetConversation([]domain.ConversationEntry)
	GetScrollOffset() int
	CanScrollUp() bool
	CanScrollDown() bool
	ToggleToolResultExpansion(index int)
	IsToolResultExpanded(index int) bool
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}

// InputComponent interface for input handling components
type InputComponent interface {
	GetInput() string
	ClearInput()
	SetPlaceholder(text string)
	GetCursor() int
	SetCursor(position int)
	SetWidth(width int)
	SetHeight(height int)
	Render() string
	HandleKey(key tea.KeyMsg) (tea.Model, tea.Cmd)
	CanHandle(key tea.KeyMsg) bool
}

// StatusComponent interface for status display components
type StatusComponent interface {
	ShowStatus(message string)
	ShowError(message string)
	ShowSpinner(message string)
	ClearStatus()
	IsShowingError() bool
	IsShowingSpinner() bool
	SetTokenUsage(usage string)
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}

// HelpBarComponent interface for bottom help bar display
type HelpBarComponent interface {
	SetShortcuts(shortcuts []shared.KeyShortcut)
	IsEnabled() bool
	SetEnabled(enabled bool)
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}

// Ensure AutocompleteImpl implements the shared interface
var _ shared.AutocompleteInterface = (*AutocompleteImpl)(nil)

// SelectionComponent interface for selection components (models, files, etc.)
type SelectionComponent interface {
	GetOptions() []string
	SetOptions(options []string)
	GetSelected() string
	GetSelectedIndex() int
	SetSelected(index int)
	IsSelected() bool
	IsCancelled() bool
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}
