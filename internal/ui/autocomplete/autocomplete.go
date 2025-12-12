package autocomplete

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	ui "github.com/inference-gateway/cli/internal/ui"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	sdk "github.com/inference-gateway/sdk"
)

// ShortcutOption represents a shortcut option for autocomplete
type ShortcutOption struct {
	Shortcut    string
	Description string
	Usage       string
}

// ShortcutRegistry interface for dependency injection
type ShortcutRegistry interface {
	GetAll() []shortcuts.Shortcut
}

// AutocompleteImpl implements inline autocomplete functionality
type AutocompleteImpl struct {
	suggestions              []ShortcutOption
	filtered                 []ShortcutOption
	selected                 int
	visible                  bool
	query                    string
	theme                    ui.Theme
	width                    int
	height                   int
	maxVisible               int
	shortcutRegistry         ShortcutRegistry
	stateManager             domain.StateManager
	lastAgentMode            domain.AgentMode
	toolService              domain.ToolService
	modelService             domain.ModelService
	pricingService           domain.PricingService
	completionMode           string
	shouldExecuteImmediately bool
}

// NewAutocomplete creates a new autocomplete component
func NewAutocomplete(theme ui.Theme, shortcutRegistry ShortcutRegistry) *AutocompleteImpl {
	return &AutocompleteImpl{
		suggestions:      []ShortcutOption{},
		filtered:         []ShortcutOption{},
		selected:         0,
		visible:          false,
		query:            "",
		theme:            theme,
		width:            80,
		maxVisible:       5,
		shortcutRegistry: shortcutRegistry,
		toolService:      nil,
	}
}

// SetToolService sets the tool service for tool autocomplete
func (a *AutocompleteImpl) SetToolService(toolService domain.ToolService) {
	a.toolService = toolService
}

// SetStateManager sets the state manager for agent mode filtering
func (a *AutocompleteImpl) SetStateManager(stateManager domain.StateManager) {
	a.stateManager = stateManager
}

// SetModelService sets the model service for model autocomplete
func (a *AutocompleteImpl) SetModelService(modelService domain.ModelService) {
	a.modelService = modelService
}

// SetPricingService sets the pricing service for model pricing display
func (a *AutocompleteImpl) SetPricingService(pricingService domain.PricingService) {
	a.pricingService = pricingService
}

// loadModels loads available models from the model service
func (a *AutocompleteImpl) loadModels() {
	if a.modelService == nil {
		return
	}

	a.suggestions = []ShortcutOption{}
	ctx := context.Background()

	models, err := a.modelService.ListModels(ctx)
	if err != nil {
		return
	}

	for _, model := range models {
		description := ""
		if a.pricingService != nil {
			description = a.pricingService.FormatModelPricing(model)
		}
		a.suggestions = append(a.suggestions, ShortcutOption{
			Shortcut:    model,
			Description: description,
			Usage:       "",
		})
	}
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
			Usage:       shortcut.GetUsage(),
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

	toolDefMap := make(map[string]sdk.ChatCompletionTool)
	for _, toolDef := range toolDefinitions {
		toolDefMap[toolDef.Function.Name] = toolDef
	}

	planModeTools := map[string]bool{
		"Read":      true,
		"Grep":      true,
		"Tree":      true,
		"TodoWrite": true,
	}

	var isInPlanMode bool
	if a.stateManager != nil {
		agentMode := a.stateManager.GetAgentMode()
		isInPlanMode = agentMode == domain.AgentModePlan
	}

	for _, toolName := range availableTools {
		if toolName == "RequestPlanApproval" {
			continue
		}

		if isInPlanMode && !planModeTools[toolName] {
			continue
		}

		var template string
		if toolDef, exists := toolDefMap[toolName]; exists {
			template = a.generateToolTemplate(toolDef)
		} else {
			template = "!!" + toolName + "("
		}

		a.suggestions = append(a.suggestions, ShortcutOption{
			Shortcut:    template,
			Description: "Execute " + toolName + " tool directly",
			Usage:       "",
		})
	}
}

// generateToolTemplate creates a complete tool template with required arguments
func (a *AutocompleteImpl) generateToolTemplate(toolDef sdk.ChatCompletionTool) string {
	template := "!!" + toolDef.Function.Name + "("

	if toolDef.Function.Parameters != nil {
		params := map[string]any(*toolDef.Function.Parameters)
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
	case strings.HasPrefix(inputText, "/model ") && cursorPos >= 7:
		if a.completionMode != "models" || len(a.suggestions) == 0 {
			a.loadModels()
			a.completionMode = "models"
		}
		a.query = inputText[7:cursorPos]
		a.filterSuggestions()
		a.visible = len(a.filtered) > 0
		if a.selected >= len(a.filtered) {
			a.selected = 0
		}
	case strings.HasPrefix(inputText, "!!") && cursorPos >= 2:
		var currentMode domain.AgentMode
		if a.stateManager != nil {
			currentMode = a.stateManager.GetAgentMode()
		}

		if currentMode != a.lastAgentMode || len(a.suggestions) == 0 || a.completionMode != "tools" {
			a.loadTools()
			a.lastAgentMode = currentMode
			a.completionMode = "tools"
		}

		a.query = inputText[2:cursorPos]
		a.filterSuggestions()
		a.visible = len(a.filtered) > 0
		if a.selected >= len(a.filtered) {
			a.selected = 0
		}
	case strings.HasPrefix(inputText, "/") && cursorPos >= 1:
		if len(a.suggestions) == 0 || a.completionMode != "shortcuts" {
			a.loadShortcuts()
			a.completionMode = "shortcuts"
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
		a.completionMode = ""
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
		if a.selected >= len(a.filtered) {
			return true, ""
		}
		return a.handleSelection()

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

// ShouldExecuteImmediately returns whether the last selected shortcut should execute immediately
func (a *AutocompleteImpl) ShouldExecuteImmediately() bool {
	return a.shouldExecuteImmediately
}

// handleSelection handles the selected autocomplete item
func (a *AutocompleteImpl) handleSelection() (bool, string) {
	selected := a.filtered[a.selected].Shortcut
	a.shouldExecuteImmediately = false

	if a.completionMode == "models" {
		selected = "/model " + selected + " "
		a.visible = false
		return true, selected
	}

	if a.completionMode == "shortcuts" && selected == "/model" {
		selected = selected + " "
		a.loadModels()
		a.completionMode = "models"
		a.query = ""
		a.filterSuggestions()
		a.visible = len(a.filtered) > 0
		return true, selected
	}

	if a.completionMode == "shortcuts" {
		if a.isNoArgShortcut(selected) {
			a.shouldExecuteImmediately = true
		} else {
			selected = selected + " "
		}
	}

	a.visible = false
	return true, selected
}

// isNoArgShortcut checks if a shortcut accepts no arguments
func (a *AutocompleteImpl) isNoArgShortcut(shortcutName string) bool {
	if a.shortcutRegistry == nil {
		return false
	}

	name := shortcutName
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	shortcuts := a.shortcutRegistry.GetAll()
	for _, s := range shortcuts {
		if s.GetName() == name {
			desc := s.GetDescription()
			return !strings.Contains(desc, "<") && !strings.Contains(desc, "[")
		}
	}

	return false
}

// SetWidth sets the width for rendering
func (a *AutocompleteImpl) SetWidth(width int) {
	a.width = width
}

// SetHeight sets the height for rendering
func (a *AutocompleteImpl) SetHeight(height int) {
	a.height = height
}

// Render returns the autocomplete suggestions as a string
func (a *AutocompleteImpl) Render() string {
	if !a.visible || len(a.filtered) == 0 {
		return ""
	}

	var b strings.Builder
	start, end := a.calculateVisibleRange()
	maxShortcutWidth := a.calculateMaxShortcutWidth()
	descWidth := a.calculateDescriptionWidth(maxShortcutWidth)

	a.renderItems(&b, start, end, maxShortcutWidth, descWidth)
	a.renderHelpText(&b)

	return b.String()
}

// calculateVisibleRange returns the start and end indices for visible items
func (a *AutocompleteImpl) calculateVisibleRange() (int, int) {
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

	return start, end
}

// calculateMaxShortcutWidth calculates the maximum width for shortcut display
func (a *AutocompleteImpl) calculateMaxShortcutWidth() int {
	maxShortcutWidth := 0
	for _, cmd := range a.filtered {
		displayText := a.getShortcutDisplayText(cmd)
		displayText = strings.TrimPrefix(displayText, "!!")
		if len(displayText) > maxShortcutWidth {
			maxShortcutWidth = len(displayText)
		}
	}

	minShortcutWidth := 15
	if a.width < 60 {
		minShortcutWidth = 10
	}

	if maxShortcutWidth < minShortcutWidth {
		maxShortcutWidth = minShortcutWidth
	}

	maxAllowedShortcutWidth := a.width / 3
	if maxShortcutWidth > maxAllowedShortcutWidth && maxAllowedShortcutWidth > minShortcutWidth {
		maxShortcutWidth = maxAllowedShortcutWidth
	}

	return maxShortcutWidth
}

// calculateDescriptionWidth calculates the width for description display
func (a *AutocompleteImpl) calculateDescriptionWidth(maxShortcutWidth int) int {
	const reservedSpace = 7
	descWidth := a.width - maxShortcutWidth - reservedSpace
	if descWidth < 20 {
		descWidth = 20
	}
	return descWidth
}

// getShortcutDisplayText returns the display text for a shortcut
func (a *AutocompleteImpl) getShortcutDisplayText(cmd ShortcutOption) string {
	if cmd.Usage != "" && cmd.Usage != cmd.Shortcut {
		return cmd.Usage
	}
	return cmd.Shortcut
}

// truncateText truncates text to fit within maxWidth
func truncateText(text string, maxWidth int) string {
	if len(text) <= maxWidth {
		return text
	}
	if maxWidth > 3 {
		return text[:maxWidth-3] + "..."
	}
	return text[:maxWidth]
}

// renderItems renders all visible autocomplete items
func (a *AutocompleteImpl) renderItems(b *strings.Builder, start, end, maxShortcutWidth, descWidth int) {
	const leftPadding = "  "

	for i := start; i < end; i++ {
		cmd := a.filtered[i]
		marker := "  "
		if i == a.selected {
			marker = "▶ "
		}

		displayText := a.getShortcutDisplayText(cmd)
		displayText = strings.TrimPrefix(displayText, "!!")
		displayText = truncateText(displayText, maxShortcutWidth)
		paddedShortcut := displayText + strings.Repeat(" ", maxShortcutWidth-len(displayText))

		description := truncateText(cmd.Description, descWidth)
		paddedDescription := description + strings.Repeat(" ", descWidth-len(description))

		a.renderItem(b, i == a.selected, leftPadding, marker, paddedShortcut, paddedDescription)

		if i < end-1 {
			b.WriteString("\n")
		}
	}
}

// renderItem renders a single autocomplete item
func (a *AutocompleteImpl) renderItem(b *strings.Builder, selected bool, leftPadding, marker, paddedShortcut, paddedDescription string) {
	if selected {
		line := fmt.Sprintf("%s%s%s%s%s │ %s%s",
			leftPadding,
			a.theme.GetAccentColor(),
			marker,
			paddedShortcut,
			a.theme.GetDimColor(),
			paddedDescription,
			colors.Reset)
		b.WriteString(line)
	} else {
		line := fmt.Sprintf("%s%s%s │ %s%s%s",
			leftPadding,
			marker,
			paddedShortcut,
			a.theme.GetDimColor(),
			paddedDescription,
			colors.Reset)
		b.WriteString(line)
	}
}

// renderHelpText renders the help text at the bottom
func (a *AutocompleteImpl) renderHelpText(b *strings.Builder) {
	const leftPadding = "  "
	helpColor := a.theme.GetDimColor()
	if len(a.filtered) > 0 {
		fmt.Fprintf(b, "\n\n%s%sTab to select, ↑↓ to navigate%s\n",
			leftPadding, helpColor, colors.Reset)
	}
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

// RefreshToolsList forces a reload of the tools list
// This should be called when MCP servers connect or disconnect
func (a *AutocompleteImpl) RefreshToolsList() {
	if len(a.suggestions) > 0 && strings.HasPrefix(a.suggestions[0].Shortcut, "!!") {
		a.suggestions = []ShortcutOption{}
	}
}

// Compile-time check to ensure AutocompleteImpl implements the interface
var _ ui.AutocompleteComponent = (*AutocompleteImpl)(nil)
