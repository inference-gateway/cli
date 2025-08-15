package ui

import (
	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
)

// Renderer interface for components that can render themselves
type Renderer interface {
	Render() string
	SetWidth(width int)
	SetHeight(height int)
}

// InputHandler interface for components that handle key input
type InputHandler interface {
	HandleKey(key tea.KeyMsg) (tea.Model, tea.Cmd)
	CanHandle(key tea.KeyMsg) bool
}

// StateUpdater interface for components that can update their state
type StateUpdater interface {
	Update(msg tea.Msg) (tea.Model, tea.Cmd)
}

// ViewComponent interface combining all UI component capabilities
type ViewComponent interface {
	Renderer
	StateUpdater
	GetID() string
}

// ConversationRenderer interface for rendering conversation history
type ConversationRenderer interface {
	Renderer
	SetConversation([]domain.ConversationEntry)
	SetScrollOffset(offset int)
	GetScrollOffset() int
	CanScrollUp() bool
	CanScrollDown() bool
	ToggleToolResultExpansion(index int)
	IsToolResultExpanded(index int) bool
}

// InputComponent interface for input handling components
type InputComponent interface {
	ViewComponent
	InputHandler
	GetInput() string
	ClearInput()
	SetPlaceholder(text string)
	GetCursor() int
	SetCursor(position int)
}

// StatusComponent interface for status display components
type StatusComponent interface {
	ViewComponent
	ShowStatus(message string)
	ShowError(message string)
	ShowSpinner(message string)
	ClearStatus()
	IsShowingError() bool
	IsShowingSpinner() bool
}

// SelectionComponent interface for selection components (models, files, etc.)
type SelectionComponent interface {
	ViewComponent
	GetOptions() []string
	SetOptions(options []string)
	GetSelected() string
	GetSelectedIndex() int
	SetSelected(index int)
	IsSelected() bool
	IsCancelled() bool
}

// Theme interface for styling components
type Theme interface {
	GetUserColor() string
	GetAssistantColor() string
	GetErrorColor() string
	GetStatusColor() string
	GetAccentColor() string
	GetDimColor() string
	GetBorderColor() string
}

// HelpBarComponent interface for bottom help bar display
type HelpBarComponent interface {
	ViewComponent
	SetShortcuts(shortcuts []KeyShortcut)
	IsEnabled() bool
	SetEnabled(enabled bool)
}

// KeyShortcut represents a keyboard shortcut with description
type KeyShortcut struct {
	Key         string
	Description string
}

// Layout interface for managing component positioning
type Layout interface {
	CalculateConversationHeight(totalHeight int) int
	CalculateInputHeight(totalHeight int) int
	CalculateStatusHeight(totalHeight int) int
	GetMargins() (top, right, bottom, left int)
}
