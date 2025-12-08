package components

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

// InputStatusBar displays input status information like model, theme, agents
type InputStatusBar struct {
	width                  int
	modelService           domain.ModelService
	themeService           domain.ThemeService
	stateManager           domain.StateManager
	configService          *config.Config
	conversationRepo       domain.ConversationRepository
	toolService            domain.ToolService
	tokenEstimator         domain.TokenEstimator
	backgroundShellService domain.BackgroundShellService
	mcpStatus              *domain.MCPServerStatus
	styleProvider          *styles.Provider
	currentInputText       string
	gitBranchCache         string
	gitBranchCacheTime     time.Time
	gitBranchCacheTTL      time.Duration
}

// NewInputStatusBar creates a new input status bar
func NewInputStatusBar(styleProvider *styles.Provider) *InputStatusBar {
	return &InputStatusBar{
		width:             80,
		styleProvider:     styleProvider,
		gitBranchCacheTTL: 5 * time.Second,
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

// SetToolService sets the tool service
func (isb *InputStatusBar) SetToolService(toolService domain.ToolService) {
	isb.toolService = toolService
}

// SetTokenEstimator sets the token estimator
func (isb *InputStatusBar) SetTokenEstimator(estimator domain.TokenEstimator) {
	isb.tokenEstimator = estimator
}

// SetBackgroundShellService sets the background shell service
func (isb *InputStatusBar) SetBackgroundShellService(service domain.BackgroundShellService) {
	isb.backgroundShellService = service
}

// UpdateMCPStatus updates the MCP server status (called by event handler)
func (isb *InputStatusBar) UpdateMCPStatus(status *domain.MCPServerStatus) {
	isb.mcpStatus = status
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
	if isb.configService != nil && !isb.configService.Chat.StatusBar.Enabled {
		return ""
	}

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

	inputMode := isb.getInputModeIndicator()
	dimColor := isb.styleProvider.GetThemeColor("dim")
	accentColor := isb.styleProvider.GetThemeColor("accent")

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
	parts := []string{}

	if isb.shouldShowIndicator("git_branch") {
		if gitBranchPart := isb.buildGitBranchIndicator(); gitBranchPart != "" {
			parts = append(parts, gitBranchPart)
		}
	}

	if isb.shouldShowIndicator("model") {
		parts = append(parts, fmt.Sprintf("Model: %s", currentModel))
	}

	if isb.shouldShowIndicator("theme") {
		if themePart := isb.buildThemeIndicator(); themePart != "" {
			parts = append(parts, themePart)
		}
	}

	if isb.shouldShowIndicator("max_output") {
		if maxOutputPart := isb.buildMaxOutputIndicator(); maxOutputPart != "" {
			parts = append(parts, maxOutputPart)
		}
	}

	if isb.shouldShowIndicator("a2a_agents") {
		if agentsPart := isb.buildA2AAgentsIndicator(); agentsPart != "" {
			parts = append(parts, agentsPart)
		}
	}

	if isb.shouldShowIndicator("tools") {
		if toolInfo := isb.getToolInfo(); toolInfo != "" {
			parts = append(parts, toolInfo)
		}
	}

	if isb.shouldShowIndicator("background_shells") {
		if backgroundInfo := isb.getBackgroundInfo(); backgroundInfo != "" {
			parts = append(parts, backgroundInfo)
		}
	}

	if isb.shouldShowIndicator("mcp") {
		if mcpPart := isb.buildMCPIndicator(); mcpPart != "" {
			parts = append(parts, mcpPart)
		}
	}

	if isb.shouldShowIndicator("context_usage") {
		if contextIndicator := isb.getContextUsageIndicator(currentModel); contextIndicator != "" {
			parts = append(parts, contextIndicator)
		}
	}

	if isb.shouldShowIndicator("session_tokens") {
		if sessionTokensPart := isb.buildSessionTokensIndicator(); sessionTokensPart != "" {
			parts = append(parts, sessionTokensPart)
		}
	}

	return strings.Join(parts, " • ")
}

// shouldShowIndicator checks if a specific indicator should be shown
func (isb *InputStatusBar) shouldShowIndicator(indicator string) bool {
	if isb.configService == nil {
		return true
	}

	indicators := isb.configService.Chat.StatusBar.Indicators
	switch indicator {
	case "model":
		return indicators.Model
	case "theme":
		return indicators.Theme
	case "max_output":
		return indicators.MaxOutput
	case "a2a_agents":
		return indicators.A2AAgents
	case "tools":
		return indicators.Tools
	case "background_shells":
		return indicators.BackgroundShells
	case "mcp":
		return indicators.MCP
	case "context_usage":
		return indicators.ContextUsage
	case "session_tokens":
		return indicators.SessionTokens
	case "git_branch":
		return indicators.GitBranch
	default:
		return true
	}
}

// buildThemeIndicator builds the theme indicator text
func (isb *InputStatusBar) buildThemeIndicator() string {
	if isb.themeService == nil {
		return ""
	}
	currentTheme := isb.themeService.GetCurrentThemeName()
	return fmt.Sprintf("Theme: %s", currentTheme)
}

// buildMaxOutputIndicator builds the max output tokens indicator text
func (isb *InputStatusBar) buildMaxOutputIndicator() string {
	if isb.configService == nil {
		return ""
	}
	maxTokens := isb.configService.Agent.MaxTokens
	if maxTokens > 0 {
		return fmt.Sprintf("Max Output: %d", maxTokens)
	}
	return ""
}

// buildA2AAgentsIndicator builds the A2A agents readiness indicator text
func (isb *InputStatusBar) buildA2AAgentsIndicator() string {
	if isb.stateManager == nil {
		return ""
	}
	if readiness := isb.stateManager.GetAgentReadiness(); readiness != nil && readiness.TotalAgents > 0 {
		return fmt.Sprintf("Agents: %d/%d", readiness.ReadyAgents, readiness.TotalAgents)
	}
	return ""
}

// buildMCPIndicator builds the MCP server status indicator text
func (isb *InputStatusBar) buildMCPIndicator() string {
	if isb.mcpStatus == nil || isb.configService == nil || len(isb.configService.MCP.Servers) == 0 {
		return ""
	}
	if isb.mcpStatus.TotalTools > 0 {
		return fmt.Sprintf("MCP: %d tools, %d/%d", isb.mcpStatus.TotalTools, isb.mcpStatus.ConnectedServers, isb.mcpStatus.TotalServers)
	}
	return fmt.Sprintf("MCP: %d/%d", isb.mcpStatus.ConnectedServers, isb.mcpStatus.TotalServers)
}

// buildSessionTokensIndicator builds the session token usage indicator text
func (isb *InputStatusBar) buildSessionTokensIndicator() string {
	if isb.conversationRepo == nil {
		return ""
	}

	stats := isb.conversationRepo.GetSessionTokens()
	totalTokens := stats.TotalTokens

	if totalTokens == 0 && isb.tokenEstimator != nil {
		messages := isb.conversationRepo.GetMessages()
		if len(messages) > 0 {
			sdkMessages := make([]sdk.Message, 0, len(messages))
			for _, entry := range messages {
				sdkMessages = append(sdkMessages, entry.Message)
			}
			totalTokens = isb.tokenEstimator.EstimateMessagesTokens(sdkMessages)
		}
	}

	if totalTokens == 0 {
		return ""
	}

	return fmt.Sprintf("Tokens: %d", totalTokens)
}

// getCurrentGitBranch returns the current git branch with caching
func (isb *InputStatusBar) getCurrentGitBranch() (string, bool) {
	if time.Since(isb.gitBranchCacheTime) < isb.gitBranchCacheTTL && isb.gitBranchCache != "" {
		return isb.gitBranchCache, true
	}

	cmd := exec.Command("git", "branch", "--show-current")
	output, err := cmd.Output()

	isb.gitBranchCacheTime = time.Now()

	if err != nil {
		isb.gitBranchCache = ""
		return "", false
	}

	branch := strings.TrimSpace(string(output))
	isb.gitBranchCache = branch
	return branch, branch != ""
}

// buildGitBranchIndicator builds the git branch indicator text
func (isb *InputStatusBar) buildGitBranchIndicator() string {
	branch, ok := isb.getCurrentGitBranch()
	if !ok || branch == "" {
		return ""
	}

	const maxBranchLength = 35
	if len(branch) > maxBranchLength {
		branch = branch[:maxBranchLength] + "..."
	}

	return fmt.Sprintf("%s %s", icons.GitBranch, branch)
}

// getToolInfo returns tool count and token information
func (isb *InputStatusBar) getToolInfo() string {
	if isb.toolService == nil || isb.tokenEstimator == nil {
		return ""
	}

	agentMode := domain.AgentModeStandard
	if isb.stateManager != nil {
		agentMode = isb.stateManager.GetAgentMode()
	}

	tokens, count := isb.tokenEstimator.GetToolStats(isb.toolService, agentMode)
	if count == 0 {
		return ""
	}

	return fmt.Sprintf("Tools: %d tokens / %d tools", tokens, count)
}

// getBackgroundInfo returns background process count information
func (isb *InputStatusBar) getBackgroundInfo() string {
	if isb.backgroundShellService == nil {
		return ""
	}

	shells := isb.backgroundShellService.GetAllShells()
	runningCount := 0
	for _, shell := range shells {
		if shell.State == domain.ShellStateRunning {
			runningCount++
		}
	}

	if runningCount == 0 {
		return ""
	}

	return fmt.Sprintf("Background: (%d)", runningCount)
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
