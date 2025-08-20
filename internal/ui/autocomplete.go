package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// CommandOption represents a command option for autocomplete
type CommandOption struct {
	Command     string
	Description string
}

// CommandRegistry interface for dependency injection
type CommandRegistry interface {
	GetAll() []commands.Command
}

// AutocompleteImpl implements inline autocomplete functionality
type AutocompleteImpl struct {
	suggestions     []CommandOption
	filtered        []CommandOption
	selected        int
	visible         bool
	query           string
	theme           Theme
	width           int
	maxVisible      int
	commandRegistry CommandRegistry
	toolService     interface {
		ListAvailableTools() []string
	}
}

// NewAutocomplete creates a new autocomplete component
func NewAutocomplete(theme Theme, commandRegistry CommandRegistry) *AutocompleteImpl {
	return &AutocompleteImpl{
		suggestions:     []CommandOption{},
		filtered:        []CommandOption{},
		selected:        0,
		visible:         false,
		query:           "",
		theme:           theme,
		width:           80,
		maxVisible:      5,
		commandRegistry: commandRegistry,
		toolService:     nil,
	}
}

// SetToolService sets the tool service for tool autocomplete
func (a *AutocompleteImpl) SetToolService(toolService interface {
	ListAvailableTools() []string
}) {
	a.toolService = toolService
}

// loadCommands loads commands from the registry
func (a *AutocompleteImpl) loadCommands() {
	if a.commandRegistry == nil {
		return
	}

	a.suggestions = []CommandOption{}
	commands := a.commandRegistry.GetAll()

	for _, cmd := range commands {
		a.suggestions = append(a.suggestions, CommandOption{
			Command:     "/" + cmd.GetName(),
			Description: cmd.GetDescription(),
		})
	}
}

// loadTools loads tools from the tool service
func (a *AutocompleteImpl) loadTools() {
	if a.toolService == nil {
		return
	}

	a.suggestions = []CommandOption{}
	tools := a.toolService.ListAvailableTools()

	for _, tool := range tools {
		a.suggestions = append(a.suggestions, CommandOption{
			Command:     "!!" + tool + "(",
			Description: "Execute " + tool + " tool directly",
		})
	}
}

// Update handles autocomplete logic
func (a *AutocompleteImpl) Update(inputText string, cursorPos int) {
	switch {
	case strings.HasPrefix(inputText, "!!") && cursorPos >= 2:
		a.loadTools()
		a.query = inputText[2:cursorPos]
		a.filterSuggestions()
		a.visible = len(a.filtered) > 0
		if a.selected >= len(a.filtered) {
			a.selected = 0
		}
	case strings.HasPrefix(inputText, "/") && cursorPos >= 1:
		if len(a.suggestions) == 0 || (len(a.suggestions) > 0 && !strings.HasPrefix(a.suggestions[0].Command, "/")) {
			a.loadCommands()
		}
		a.query = inputText[1:cursorPos]
		a.filterSuggestions()
		a.visible = len(a.filtered) > 0
		if a.selected >= len(a.filtered) {
			a.selected = 0
		}
	default:
		a.visible = false
		a.filtered = []CommandOption{}
		a.selected = 0
	}
}

// filterSuggestions filters commands based on current query
func (a *AutocompleteImpl) filterSuggestions() {
	a.filtered = []CommandOption{}

	if a.query == "" {
		a.filtered = a.suggestions
		return
	}

	for _, cmd := range a.suggestions {
		var commandName string
		if strings.HasPrefix(cmd.Command, "!!") {
			commandName = strings.TrimPrefix(cmd.Command, "!!")
			commandName = strings.TrimSuffix(commandName, "(")
		} else {
			commandName = strings.TrimPrefix(cmd.Command, "/")
		}

		if strings.HasPrefix(strings.ToLower(commandName), strings.ToLower(a.query)) {
			a.filtered = append(a.filtered, cmd)
		}
	}
}

// HandleKey processes key input for autocomplete navigation
func (a *AutocompleteImpl) HandleKey(key tea.KeyMsg) (bool, string) {
	if !a.visible || len(a.filtered) == 0 {
		return false, ""
	}

	switch key.String() {
	case "up", "ctrl+p":
		if a.selected > 0 {
			a.selected--
		} else {
			a.selected = len(a.filtered) - 1
		}
		return true, ""

	case "down", "ctrl+n":
		if a.selected < len(a.filtered)-1 {
			a.selected++
		} else {
			a.selected = 0
		}
		return true, ""

	case "tab", "enter":
		if a.selected < len(a.filtered) {
			selected := a.filtered[a.selected].Command
			a.visible = false
			return true, selected
		}
		return true, ""

	case "esc":
		a.visible = false
		return true, ""
	}

	return false, ""
}

// IsVisible returns whether autocomplete is currently visible
func (a *AutocompleteImpl) IsVisible() bool {
	return a.visible
}

// SetWidth sets the width for rendering
func (a *AutocompleteImpl) SetWidth(width int) {
	a.width = width
}

// Render returns the autocomplete suggestions as a string
func (a *AutocompleteImpl) Render() string {
	if !a.visible || len(a.filtered) == 0 {
		return ""
	}

	var b strings.Builder

	start := 0
	end := len(a.filtered)

	if len(a.filtered) > a.maxVisible {
		if a.selected >= a.maxVisible {
			start = a.selected - a.maxVisible + 1
		}
		end = start + a.maxVisible
		if end > len(a.filtered) {
			end = len(a.filtered)
			start = end - a.maxVisible
		}
	}

	for i := start; i < end; i++ {
		cmd := a.filtered[i]
		prefix := "  "

		if i == a.selected {
			prefix = fmt.Sprintf("%s▶ %s", a.theme.GetAccentColor(), shared.Reset())
		}

		line := fmt.Sprintf("%s %-12s %s%s%s",
			prefix,
			cmd.Command,
			a.theme.GetDimColor(),
			cmd.Description,
			shared.Reset())

		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	helpColor := a.theme.GetDimColor()
	if len(a.filtered) > 0 {
		b.WriteString(fmt.Sprintf("\n\n%s  Tab to select, ↑↓ to navigate%s\n",
			helpColor, shared.Reset()))
	}

	return b.String()
}

// GetSelectedCommand returns the currently selected command
func (a *AutocompleteImpl) GetSelectedCommand() string {
	if a.visible && a.selected < len(a.filtered) {
		return a.filtered[a.selected].Command
	}
	return ""
}

// Hide hides the autocomplete
func (a *AutocompleteImpl) Hide() {
	a.visible = false
}
