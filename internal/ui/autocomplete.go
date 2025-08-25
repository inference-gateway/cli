package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/shortcuts"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// ShortcutOption represents a shortcut option for autocomplete
type ShortcutOption struct {
	Shortcut    string
	Description string
}

// ShortcutRegistry interface for dependency injection
type ShortcutRegistry interface {
	GetAll() []shortcuts.Shortcut
}

// AutocompleteImpl implements inline autocomplete functionality
type AutocompleteImpl struct {
	suggestions     []ShortcutOption
	filtered        []ShortcutOption
	selected        int
	visible         bool
	query           string
	theme           Theme
	width           int
	maxVisible      int
	shortcutRegistry ShortcutRegistry
	toolService     interface {
		ListAvailableTools() []string
		ListTools() []domain.ToolDefinition
	}
}

// NewAutocomplete creates a new autocomplete component
func NewAutocomplete(theme Theme, shortcutRegistry ShortcutRegistry) *AutocompleteImpl {
	return &AutocompleteImpl{
		suggestions:     []ShortcutOption{},
		filtered:        []ShortcutOption{},
		selected:        0,
		visible:         false,
		query:           "",
		theme:           theme,
		width:           80,
		maxVisible:      5,
		shortcutRegistry: shortcutRegistry,
		toolService:     nil,
	}
}

// SetToolService sets the tool service for tool autocomplete
func (a *AutocompleteImpl) SetToolService(toolService interface {
	ListAvailableTools() []string
	ListTools() []domain.ToolDefinition
}) {
	a.toolService = toolService
}

// loadShortcuts loads shortcuts from the registry
func (a *AutocompleteImpl) loadShortcuts() {
	if a.shortcutRegistry == nil {
		return
	}

	a.suggestions = []ShortcutOption{}
	shortcuts := a.shortcutRegistry.GetAll()

	for _, shortcut := range shortcuts {
		a.suggestions = append(a.suggestions, ShortcutOption{
			Shortcut:    "/" + shortcut.GetName(),
			Description: shortcut.GetDescription(),
		})
	}
}

// loadTools loads tools from the tool service with their required parameters
func (a *AutocompleteImpl) loadTools() {
	if a.toolService == nil {
		return
	}

	a.suggestions = []ShortcutOption{}

	availableTools := a.toolService.ListAvailableTools()
	toolDefinitions := a.toolService.ListTools()

	toolDefMap := make(map[string]domain.ToolDefinition)
	for _, toolDef := range toolDefinitions {
		toolDefMap[toolDef.Name] = toolDef
	}

	for _, toolName := range availableTools {
		var template string
		if toolDef, exists := toolDefMap[toolName]; exists {
			template = a.generateToolTemplate(toolDef)
		} else {
			template = "!!" + toolName + "("
		}

		a.suggestions = append(a.suggestions, ShortcutOption{
			Shortcut:     template,
			Description: "Execute " + toolName + " tool directly",
		})
	}
}

// generateToolTemplate creates a complete tool template with required arguments
func (a *AutocompleteImpl) generateToolTemplate(toolDef domain.ToolDefinition) string {
	template := "!!" + toolDef.Name + "("

	if params, ok := toolDef.Parameters.(map[string]any); ok {
		requiredArgs := a.extractRequiredArguments(params)
		if len(requiredArgs) > 0 {
			template += strings.Join(requiredArgs, ", ")
		}
	}

	template += ")"
	return template
}

// extractRequiredArguments extracts required arguments from parameters
func (a *AutocompleteImpl) extractRequiredArguments(params map[string]any) []string {
	var requiredArgs []string

	var properties map[string]any
	if props, ok := params["properties"].(map[string]any); ok {
		properties = props
	}

	if requiredRaw, exists := params["required"]; exists {
		switch required := requiredRaw.(type) {
		case []any:
			requiredArgs = a.processAnySlice(required, properties)
		case []string:
			requiredArgs = a.processStringSlice(required, properties)
		}
	}

	return requiredArgs
}

// processAnySlice processes a slice of any type for required arguments
func (a *AutocompleteImpl) processAnySlice(required []any, properties map[string]any) []string {
	var args []string
	for _, req := range required {
		if reqStr, ok := req.(string); ok {
			argTemplate := a.generateArgumentTemplate(reqStr, properties)
			if argTemplate != "" {
				args = append(args, argTemplate)
			}
		}
	}
	return args
}

// processStringSlice processes a slice of strings for required arguments
func (a *AutocompleteImpl) processStringSlice(required []string, properties map[string]any) []string {
	var args []string
	for _, req := range required {
		argTemplate := a.generateArgumentTemplate(req, properties)
		if argTemplate != "" {
			args = append(args, argTemplate)
		}
	}
	return args
}

// generateArgumentTemplate creates the appropriate template for a parameter based on its type
func (a *AutocompleteImpl) generateArgumentTemplate(paramName string, properties map[string]any) string {
	if properties == nil {
		return paramName + "=\"\""
	}

	if paramDef, ok := properties[paramName].(map[string]any); ok {
		if paramType, ok := paramDef["type"].(string); ok {
			switch paramType {
			case "string":
				return paramName + "=\"\""
			case "integer", "number":
				return ""
			case "boolean":
				return paramName + "=false"
			default:
				return paramName + "=\"\""
			}
		}
	}

	return paramName + "=\"\""
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
		if len(a.suggestions) == 0 || (len(a.suggestions) > 0 && !strings.HasPrefix(a.suggestions[0].Shortcut, "/")) {
			a.loadShortcuts()
		}
		a.query = inputText[1:cursorPos]
		a.filterSuggestions()
		a.visible = len(a.filtered) > 0
		if a.selected >= len(a.filtered) {
			a.selected = 0
		}
	default:
		a.visible = false
		a.filtered = []ShortcutOption{}
		a.selected = 0
	}
}

// filterSuggestions filters commands based on current query
func (a *AutocompleteImpl) filterSuggestions() {
	a.filtered = []ShortcutOption{}

	if a.query == "" {
		a.filtered = a.suggestions
		return
	}

	for _, cmd := range a.suggestions {
		var commandName string
		if name, found := strings.CutPrefix(cmd.Shortcut, "!!"); found {
			commandName = name
			if idx := strings.Index(commandName, "("); idx != -1 {
				commandName = commandName[:idx]
			}
		} else {
			commandName = strings.TrimPrefix(cmd.Shortcut, "/")
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
			selected := a.filtered[a.selected].Shortcut
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
			prefix = fmt.Sprintf("%s▶ %s", a.theme.GetAccentColor(), colors.Reset)
		}

		line := fmt.Sprintf("%s %-12s %s%s%s",
			prefix,
			cmd.Shortcut,
			a.theme.GetDimColor(),
			cmd.Description,
			colors.Reset)

		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	helpColor := a.theme.GetDimColor()
	if len(a.filtered) > 0 {
		b.WriteString(fmt.Sprintf("\n\n%s  Tab to select, ↑↓ to navigate%s\n",
			helpColor, colors.Reset))
	}

	return b.String()
}

// GetSelectedShortcut returns the currently selected shortcut
func (a *AutocompleteImpl) GetSelectedShortcut() string {
	if a.visible && a.selected < len(a.filtered) {
		return a.filtered[a.selected].Shortcut
	}
	return ""
}

// Hide hides the autocomplete
func (a *AutocompleteImpl) Hide() {
	a.visible = false
}
