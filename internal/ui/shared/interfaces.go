package shared

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
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
	SetTextSelectionMode(enabled bool)
	IsTextSelectionMode() bool
}

// StatusComponent interface for status display
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

// FormatWarning creates a properly formatted warning message
func FormatWarning(message string) string {
	return fmt.Sprintf("⚠️ %s", message)
}
