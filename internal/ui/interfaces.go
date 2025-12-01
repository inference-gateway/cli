package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// KeyShortcut represents a keyboard shortcut with description
type KeyShortcut struct {
	Key         string
	Description string
}

// ScrollDirection represents different scroll directions
type ScrollDirection int

const (
	ScrollUp ScrollDirection = iota
	ScrollDown
	ScrollToTop
	ScrollToBottom
)

// AutocompleteInterface defines the interface for autocomplete functionality
type AutocompleteInterface interface {
	Update(inputText string, cursorPos int)
	HandleKey(key tea.KeyMsg) (bool, string)
	IsVisible() bool
	SetWidth(width int)
	Render() string
	GetSelectedShortcut() string
	Hide()
}

// Theme interface for UI theming
type Theme interface {
	GetUserColor() string
	GetAssistantColor() string
	GetErrorColor() string
	GetSuccessColor() string
	GetStatusColor() string
	GetAccentColor() string
	GetDimColor() string
	GetBorderColor() string
	GetDiffAddColor() string
	GetDiffRemoveColor() string
}

// ConversationRenderer interface for conversation display
type ConversationRenderer interface {
	SetConversation([]domain.ConversationEntry)
	GetScrollOffset() int
	CanScrollUp() bool
	CanScrollDown() bool
	ToggleToolResultExpansion(index int)
	ToggleAllToolResultsExpansion()
	IsToolResultExpanded(index int) bool
	ToggleRawFormat()
	IsRawFormat() bool
	ResetUserScroll()
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}

// InputComponent interface for input handling
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
	IsAutocompleteVisible() bool
	TryHandleAutocomplete(key tea.KeyMsg) (handled bool, completion string)
	AddImageAttachment(image domain.ImageAttachment)
	GetImageAttachments() []domain.ImageAttachment
	ClearImageAttachments()
	AddToHistory(text string) error
}

// StatusComponent interface for status display
type StatusComponent interface {
	ShowStatus(message string)
	ShowError(message string)
	ShowSpinner(message string)
	ClearStatus()
	IsShowingError() bool
	IsShowingSpinner() bool
	SetWidth(width int)
	SetHeight(height int)
	Render() string
	SaveCurrentState()
	RestoreSavedState() tea.Cmd
	HasSavedState() bool
}

// HelpBarComponent interface for help bar
type HelpBarComponent interface {
	SetShortcuts(shortcuts []KeyShortcut)
	IsEnabled() bool
	SetEnabled(enabled bool)
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}

// ApprovalComponent interface for approval display
type ApprovalComponent interface {
	SetWidth(width int)
	SetHeight(height int)
	Render(toolExecution *domain.ToolExecutionSession, selectedIndex int) string
}

// DefaultTheme provides a concrete implementation of the Theme interface
type DefaultTheme struct{}

func NewDefaultTheme() *DefaultTheme { return &DefaultTheme{} }

func (t *DefaultTheme) GetUserColor() string       { return colors.UserColor.ANSI }
func (t *DefaultTheme) GetAssistantColor() string  { return colors.AssistantColor.ANSI }
func (t *DefaultTheme) GetErrorColor() string      { return colors.ErrorColor.ANSI }
func (t *DefaultTheme) GetSuccessColor() string    { return colors.SuccessColor.ANSI }
func (t *DefaultTheme) GetStatusColor() string     { return colors.StatusColor.ANSI }
func (t *DefaultTheme) GetAccentColor() string     { return colors.AccentColor.ANSI }
func (t *DefaultTheme) GetDimColor() string        { return colors.DimColor.ANSI }
func (t *DefaultTheme) GetBorderColor() string     { return colors.BorderColor.ANSI }
func (t *DefaultTheme) GetDiffAddColor() string    { return colors.DiffAddColor.ANSI }
func (t *DefaultTheme) GetDiffRemoveColor() string { return colors.DiffRemoveColor.ANSI }

// SelectionComponent is specific to UI layer (not duplicated in shared)
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

// Layout calculation utilities
func CalculateConversationHeight(totalHeight int) int {
	inputHeight := CalculateInputHeight(totalHeight)
	statusHeight := CalculateStatusHeight(totalHeight)

	extraLines := 5
	if totalHeight < 12 {
		extraLines = 3
	}

	conversationHeight := totalHeight - inputHeight - statusHeight - extraLines

	minConversationHeight := 3
	if conversationHeight < minConversationHeight {
		conversationHeight = minConversationHeight
	}

	return conversationHeight
}

func CalculateInputHeight(totalHeight int) int {
	if totalHeight < 8 {
		return 2
	}
	if totalHeight < 12 {
		return 3
	}
	return 4
}

func CalculateStatusHeight(totalHeight int) int {
	if totalHeight < 8 {
		return 0
	}
	if totalHeight < 12 {
		return 1
	}
	return 2
}
