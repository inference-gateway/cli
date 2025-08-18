package ui

import (
	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

type KeyShortcut = shared.KeyShortcut

// Theme is an alias to the shared Theme interface
type Theme = shared.Theme

type DefaultTheme struct{}

func NewDefaultTheme() *DefaultTheme { return &DefaultTheme{} }

func (t *DefaultTheme) GetUserColor() string       { return shared.UserColor.ANSI }
func (t *DefaultTheme) GetAssistantColor() string  { return shared.AssistantColor.ANSI }
func (t *DefaultTheme) GetErrorColor() string      { return shared.ErrorColor.ANSI }
func (t *DefaultTheme) GetStatusColor() string     { return shared.StatusColor.ANSI }
func (t *DefaultTheme) GetAccentColor() string     { return shared.AccentColor.ANSI }
func (t *DefaultTheme) GetDimColor() string        { return shared.DimColor.ANSI }
func (t *DefaultTheme) GetBorderColor() string     { return shared.BorderColor.ANSI }
func (t *DefaultTheme) GetDiffAddColor() string    { return shared.DiffAddColor.ANSI }
func (t *DefaultTheme) GetDiffRemoveColor() string { return shared.DiffRemoveColor.ANSI }

type ConversationRenderer interface {
	SetConversation([]domain.ConversationEntry)
	GetScrollOffset() int
	CanScrollUp() bool
	CanScrollDown() bool
	ToggleToolResultExpansion(index int)
	ToggleAllToolResultsExpansion()
	IsToolResultExpanded(index int) bool
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}

type InputComponent interface {
	GetInput() string
	ClearInput()
	SetPlaceholder(text string)
	GetCursor() int
	SetCursor(position int)
	SetText(text string)
	SetWidth(width int)
	SetHeight(height int)
	Render() string
	HandleKey(key tea.KeyMsg) (tea.Model, tea.Cmd)
	CanHandle(key tea.KeyMsg) bool
	NavigateHistoryUp()
	NavigateHistoryDown()
}

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

type HelpBarComponent interface {
	SetShortcuts(shortcuts []shared.KeyShortcut)
	IsEnabled() bool
	SetEnabled(enabled bool)
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}

var _ shared.AutocompleteInterface = (*AutocompleteImpl)(nil)

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

type ApprovalComponent interface {
	SetWidth(width int)
	SetHeight(height int)
	Render(toolExecution *domain.ToolExecutionSession, selectedIndex int) string
}
