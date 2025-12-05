package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// InputStatusBar displays input status information like model, theme, agents
type InputStatusBar struct {
	width            int
	modelService     domain.ModelService
	themeService     domain.ThemeService
	stateManager     domain.StateManager
	configService    *config.Config
	conversationRepo domain.ConversationRepository
	mcpClient        domain.MCPClient
	styleProvider    *styles.Provider
	currentInputText string
}

// NewInputStatusBar creates a new input status bar
func NewInputStatusBar(styleProvider *styles.Provider) *InputStatusBar {
	return &InputStatusBar{
		width:         80,
		styleProvider: styleProvider,
	}
}

// SetModelService sets the model service
func (isb *InputStatusBar) SetModelService(modelService domain.ModelService) {
	isb.modelService = modelService
}

// SetThemeService sets the theme service
func (isb *InputStatusBar) SetThemeService(themeService domain.ThemeService) {
	isb.themeService = themeService
}

// SetStateManager sets the state manager
func (isb *InputStatusBar) SetStateManager(stateManager domain.StateManager) {
	isb.stateManager = stateManager
}

// SetConfigService sets the config service
func (isb *InputStatusBar) SetConfigService(configService *config.Config) {
	isb.configService = configService
}

// SetConversationRepo sets the conversation repository
func (isb *InputStatusBar) SetConversationRepo(repo domain.ConversationRepository) {
	isb.conversationRepo = repo
}

// SetMCPClient sets the MCP client
func (isb *InputStatusBar) SetMCPClient(mcpClient domain.MCPClient) {
	isb.mcpClient = mcpClient
}

// SetInputText sets the current input text for mode detection
func (isb *InputStatusBar) SetInputText(text string) {
	isb.currentInputText = text
}

func (isb *InputStatusBar) SetWidth(width int) {
	isb.width = width
}

func (isb *InputStatusBar) SetHeight(height int) {
	// Status bar has fixed height
}

func (isb *InputStatusBar) Render() string {
	renderedLeft := isb.buildLeftText()
	modeIndicator := isb.getAgentModeIndicator()
	availableWidth := isb.width - 2 - 2
	return isb.combineLeftAndRight(renderedLeft, modeIndicator, availableWidth)
}

// buildLeftText constructs and styles the left portion of the status bar
func (isb *InputStatusBar) buildLeftText() string {
	if isb.styleProvider == nil {
		return ""
	}

	modelInfo := isb.getModelInfo()
	if modelInfo == "" {
		return ""
	}

	// Combine input mode with model info if present
	inputMode := isb.getInputModeIndicator()
	dimColor := isb.styleProvider.GetThemeColor("dim")
	accentColor := isb.styleProvider.GetThemeColor("accent")

	// If there's an input mode, apply accent color to mode and dim color to rest
	if inputMode != "" {
		return isb.styleProvider.RenderWithColor(inputMode, accentColor) + " " + isb.styleProvider.RenderWithColor(modelInfo, dimColor)
	}

	return isb.styleProvider.RenderWithColor(modelInfo, dimColor)
}

// getModelInfo returns the model display information
func (isb *InputStatusBar) getModelInfo() string {
	if isb.modelService == nil {
		return ""
	}
	currentModel := isb.modelService.GetCurrentModel()
	if currentModel == "" {
		return ""
	}
	return isb.buildModelDisplayText(currentModel)
}

func (isb *InputStatusBar) buildModelDisplayText(currentModel string) string {
	parts := []string{fmt.Sprintf("Model: %s", currentModel)}

	if isb.themeService != nil {
		currentTheme := isb.themeService.GetCurrentThemeName()
		parts = append(parts, fmt.Sprintf("Theme: %s", currentTheme))
	}

	if isb.configService != nil {
		maxTokens := isb.configService.Agent.MaxTokens
		if maxTokens > 0 {
			parts = append(parts, fmt.Sprintf("Max Output: %d", maxTokens))
		}
	}

	if isb.stateManager != nil {
		if readiness := isb.stateManager.GetAgentReadiness(); readiness != nil && readiness.TotalAgents > 0 {
			parts = append(parts, fmt.Sprintf("Agents: %d/%d", readiness.ReadyAgents, readiness.TotalAgents))
		}
	}

	if isb.mcpClient != nil {
		if status := isb.mcpClient.GetMCPServerStatus(); status != nil && status.TotalServers > 0 {
			parts = append(parts, fmt.Sprintf("MCP: %d/%d", status.ConnectedServers, status.TotalServers))
		}
	}

	if contextIndicator := isb.getContextUsageIndicator(currentModel); contextIndicator != "" {
		parts = append(parts, contextIndicator)
	}

	return strings.Join(parts, " • ")
}

// getContextUsageIndicator returns a context usage indicator string
func (isb *InputStatusBar) getContextUsageIndicator(model string) string {
	if isb.conversationRepo == nil {
		return ""
	}

	stats := isb.conversationRepo.GetSessionTokens()
	currentContextSize := stats.LastInputTokens
	if currentContextSize == 0 {
		return ""
	}

	contextWindow := isb.estimateContextWindow(model)
	if contextWindow == 0 {
		return ""
	}

	usagePercent := float64(currentContextSize) * 100 / float64(contextWindow)

	displayPercent := usagePercent
	if displayPercent > 100 {
		displayPercent = 100
	}

	if usagePercent >= 90 {
		return fmt.Sprintf("Context: %.0f%% FULL", displayPercent)
	} else if usagePercent >= 75 {
		return fmt.Sprintf("Context: %.0f%% HIGH", displayPercent)
	} else if usagePercent >= 50 {
		return fmt.Sprintf("Context: %.0f%%", displayPercent)
	}

	return ""
}

// estimateContextWindow returns an estimated context window size based on model name
func (isb *InputStatusBar) estimateContextWindow(model string) int {
	return models.EstimateContextWindow(model)
}

// getInputModeIndicator returns a mode indicator for bash/tools mode (plain text, no styling)
func (isb *InputStatusBar) getInputModeIndicator() string {
	if isb.currentInputText == "" {
		return ""
	}

	isToolsMode := strings.HasPrefix(isb.currentInputText, "!!")
	isBashMode := strings.HasPrefix(isb.currentInputText, "!") && !isToolsMode

	if isToolsMode {
		return "Tools mode •"
	} else if isBashMode {
		return "Bash mode •"
	}

	return ""
}

// getAgentModeIndicator returns a compact mode indicator for display on the right side
func (isb *InputStatusBar) getAgentModeIndicator() string {
	if isb.stateManager == nil {
		return ""
	}

	agentMode := isb.stateManager.GetAgentMode()
	if agentMode == domain.AgentModeStandard {
		return ""
	}

	var modeText string
	switch agentMode {
	case domain.AgentModePlan:
		modeText = "▶ PLAN"
	case domain.AgentModeAutoAccept:
		modeText = "▸ AUTO"
	}

	return isb.styleProvider.RenderStyledText(
		modeText,
		styles.StyleOptions{
			Foreground: isb.styleProvider.GetThemeColor("accent"),
			Bold:       true,
		},
	)
}

// combineLeftAndRight combines the left text and right mode indicator with appropriate spacing
func (isb *InputStatusBar) combineLeftAndRight(renderedLeft string, modeIndicator string, availableWidth int) string {
	const leftPadding = "  "

	if renderedLeft == "" && modeIndicator == "" {
		return leftPadding + "\u00A0"
	}

	if renderedLeft == "" {
		return leftPadding + isb.formatRightOnly(modeIndicator, availableWidth)
	}

	if modeIndicator == "" {
		return leftPadding + renderedLeft
	}

	return leftPadding + isb.formatBothSides(renderedLeft, modeIndicator, availableWidth)
}

// formatBothSides formats content when both left and right text are present
func (isb *InputStatusBar) formatBothSides(renderedLeft, modeIndicator string, availableWidth int) string {
	leftWidth := isb.styleProvider.GetWidth(renderedLeft)
	rightWidth := isb.styleProvider.GetWidth(modeIndicator)
	spacingWidth := availableWidth - leftWidth - rightWidth

	if spacingWidth > 0 {
		return renderedLeft + strings.Repeat(" ", spacingWidth) + modeIndicator
	}
	return renderedLeft + " " + modeIndicator
}

// formatRightOnly formats content when only right text is present
func (isb *InputStatusBar) formatRightOnly(modeIndicator string, availableWidth int) string {
	rightWidth := isb.styleProvider.GetWidth(modeIndicator)
	spacingWidth := availableWidth - rightWidth
	if spacingWidth > 0 {
		return strings.Repeat(" ", spacingWidth) + modeIndicator
	}
	return modeIndicator
}

// Bubble Tea interface
func (isb *InputStatusBar) Init() tea.Cmd { return nil }

func (isb *InputStatusBar) View() string { return isb.Render() }

func (isb *InputStatusBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		isb.SetWidth(windowMsg.Width)
	}
	return isb, nil
}
