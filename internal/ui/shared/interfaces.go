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