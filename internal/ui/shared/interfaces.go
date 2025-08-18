package shared

import "github.com/charmbracelet/bubbletea"

// AutocompleteInterface defines the interface for autocomplete functionality
type AutocompleteInterface interface {
	Update(inputText string, cursorPos int)
	HandleKey(key tea.KeyMsg) (bool, string)
	IsVisible() bool
	SetWidth(width int)
	Render() string
	GetSelectedCommand() string
	Hide()
}

// Theme interface for UI theming
type Theme interface {
	GetUserColor() string
	GetAssistantColor() string
	GetErrorColor() string
	GetStatusColor() string
	GetAccentColor() string
	GetDimColor() string
	GetBorderColor() string
	GetDiffAddColor() string
	GetDiffRemoveColor() string
}
