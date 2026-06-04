package components

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// InputStatusBar displays input status information like model, theme, agents
type InputStatusBar struct {
	width                  int
	modelService           domain.ModelService
	themeService           domain.ThemeService
	stateManager           domain.StateManager
	config                 *config.Config
	conversationRepo       domain.ConversationRepository
	toolService            domain.ToolService
	tokenEstimator         domain.TokenEstimator
	backgroundShellService domain.BackgroundShellService
	backgroundTaskService  domain.BackgroundTaskService
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

// SetConfig sets the config for the status bar
func (isb *InputStatusBar) SetConfig(cfg *config.Config) {
	isb.config = cfg
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

// SetBackgroundTaskService sets the background task service
func (isb *InputStatusBar) SetBackgroundTaskService(service domain.BackgroundTaskService) {
	isb.backgroundTaskService = service
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
	if isb.config != nil && !isb.config.Chat.StatusBar.Enabled {
		return ""
	}

	lines := isb.buildStatusLines()
	return strings.Join(lines, "\n")
}

// buildStatusLines builds the status bar content. Indicators are packed onto the
// top row(s); the git branch always gets its own dedicated row directly below,
// left-aligned with the indicators. The bar stays within maxLines rows total -
// when a branch is shown the indicators get maxLines-1 rows, otherwise maxLines.
func (isb *InputStatusBar) buildStatusLines() []string {
	const (
		maxLines       = 2
		leftPadding    = "  "
		separatorWidth = 3
	)

	if isb.styleProvider == nil {
		return []string{leftPadding + "\u00A0"}
	}

	dimColor := isb.styleProvider.GetThemeColor("dim")
	availableWidth := isb.width - len(leftPadding) - 2

	branchLine := isb.buildGitBranchLine(leftPadding, dimColor)

	parts := isb.getAllIndicatorParts()
	if len(parts) == 0 && branchLine == "" {
		return []string{leftPadding + "\u00A0"}
	}

	indicatorMaxLines := maxLines
	if branchLine != "" {
		indicatorMaxLines = maxLines - 1
	}

	var lines []string
	if len(parts) > 0 && indicatorMaxLines > 0 {
		lineGroups := isb.splitPartsIntoLines(parts, availableWidth, indicatorMaxLines, separatorWidth)
		lineGroups = capIndicatorLines(lineGroups, indicatorMaxLines)
		for _, lineItems := range lineGroups {
			lineText := strings.Join(lineItems, " • ")
			renderedLine := isb.styleProvider.RenderWithColor(lineText, dimColor)
			lines = append(lines, leftPadding+renderedLine)
		}
	}

	if branchLine != "" {
		lines = append(lines, branchLine)
	}

	if len(lines) == 0 {
		return []string{leftPadding + "\u00A0"}
	}

	return lines
}

// getAllIndicatorParts returns all indicator parts as a slice
func (isb *InputStatusBar) getAllIndicatorParts() []string {
	if isb.modelService == nil {
		return []string{}
	}

	currentModel := isb.modelService.GetCurrentModel()
	if currentModel == "" {
		return []string{}
	}

	return isb.buildIndicatorParts(currentModel)
}

// buildIndicatorParts builds individual indicator parts without joining them.
// The git branch is intentionally excluded here - it is rendered on its own
// dedicated row by buildGitBranchLine so it never competes with these indicators
// for horizontal space.
func (isb *InputStatusBar) buildIndicatorParts(currentModel string) []string {
	parts := []string{}

	if isb.shouldShowIndicator("model") {
		parts = append(parts, currentModel)
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

	if isb.shouldShowIndicator("a2a_tasks") {
		if a2aInfo := isb.getA2ATaskInfo(); a2aInfo != "" {
			parts = append(parts, a2aInfo)
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

	if isb.shouldShowIndicator("cost") {
		if costPart := isb.buildCostIndicator(); costPart != "" {
			parts = append(parts, costPart)
		}
	}

	return parts
}

// splitPartsIntoLines splits indicator parts into line groups based on available width
func (isb *InputStatusBar) splitPartsIntoLines(parts []string, availableWidth, maxLines, separatorWidth int) [][]string {
	var lineGroups [][]string
	currentLineItems := []string{}
	currentLineWidth := 0

	for i, part := range parts {
		itemWidth := len(part)
		separatorLen := 0

		if len(currentLineItems) > 0 {
			separatorLen = separatorWidth
		}

		needsNewLine := len(currentLineItems) > 0 && currentLineWidth+separatorLen+itemWidth > availableWidth
		if needsNewLine {
			lineGroups = append(lineGroups, currentLineItems)
			currentLineItems = []string{part}
			currentLineWidth = itemWidth

			if isb.shouldAddOverflowAndBreak(len(lineGroups), maxLines, i, len(parts), currentLineWidth, separatorWidth, availableWidth, &currentLineItems) {
				break
			}
		} else {
			currentLineItems = append(currentLineItems, part)
			currentLineWidth += separatorLen + itemWidth
		}
	}

	if len(currentLineItems) > 0 {
		lineGroups = append(lineGroups, currentLineItems)
	}

	return lineGroups
}

// capIndicatorLines hard-caps the indicator rows at maxLines. splitPartsIntoLines
// can emit one row beyond its budget at the cap boundary; this guarantees the
// indicators never exceed their share of the status bar so the branch row keeps
// the bar at a stable height. When rows are dropped, an ellipsis is appended to
// the last kept row to signal the overflow.
func capIndicatorLines(lineGroups [][]string, maxLines int) [][]string {
	if maxLines <= 0 {
		return nil
	}
	if len(lineGroups) <= maxLines {
		return lineGroups
	}

	capped := lineGroups[:maxLines]
	last := capped[maxLines-1]
	if n := len(last); n == 0 || (last[n-1] != "…" && last[n-1] != "...") {
		capped[maxLines-1] = append(last, "…")
	}
	return capped
}

// shouldAddOverflowAndBreak checks if we've reached max lines and adds overflow indicator if needed
func (isb *InputStatusBar) shouldAddOverflowAndBreak(currentLines, maxLines, currentIndex, totalParts, lineWidth, separatorWidth, availableWidth int, lineItems *[]string) bool {
	if currentLines < maxLines {
		return false
	}

	if currentIndex < totalParts-1 {
		overflowWidth := 3
		if lineWidth+separatorWidth+overflowWidth <= availableWidth {
			*lineItems = append(*lineItems, "...")
		}
	}

	return true
}

func (isb *InputStatusBar) buildModelDisplayText(currentModel string) string {
	parts := []string{}

	if isb.shouldShowIndicator("git_branch") {
		if gitBranchPart := isb.buildGitBranchIndicator(); gitBranchPart != "" {
			parts = append(parts, gitBranchPart)
		}
	}

	if isb.shouldShowIndicator("model") {
		parts = append(parts, currentModel)
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

	if isb.shouldShowIndicator("a2a_tasks") {
		if a2aInfo := isb.getA2ATaskInfo(); a2aInfo != "" {
			parts = append(parts, a2aInfo)
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
	if isb.config == nil {
		return true
	}

	indicators := isb.config.Chat.StatusBar.Indicators
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
	case "a2a_tasks":
		return indicators.A2ATasks
	case "mcp":
		return indicators.MCP
	case "context_usage":
		return indicators.ContextUsage
	case "session_tokens":
		return indicators.SessionTokens
	case "cost":
		return indicators.Cost
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
	return currentTheme
}

// buildMaxOutputIndicator builds the max output tokens indicator text
func (isb *InputStatusBar) buildMaxOutputIndicator() string {
	if isb.config == nil {
		return ""
	}
	maxTokens := isb.config.Agent.MaxTokens
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
	if isb.mcpStatus == nil || isb.config == nil || len(isb.config.MCP.Servers) == 0 {
		return ""
	}
	if isb.mcpStatus.TotalTools > 0 {
		return fmt.Sprintf("🔌 %d/%d (%d)", isb.mcpStatus.ConnectedServers, isb.mcpStatus.TotalServers, isb.mcpStatus.TotalTools)
	}
	return fmt.Sprintf("🔌 %d/%d", isb.mcpStatus.ConnectedServers, isb.mcpStatus.TotalServers)
}

// buildSessionTokensIndicator builds the cumulative input-tokens indicator.
// Shows the total input tokens billed across the entire session (the same
// number that drives the cost calculation). This is a cumulative running
// total, not the size of the current context window - the Context indicator
// uses LastInputTokens for that. Falls back to a tokenizer estimate of the
// current message buffer when the provider has not returned usage yet.
func (isb *InputStatusBar) buildSessionTokensIndicator() string {
	if isb.conversationRepo == nil {
		return ""
	}

	totalTokens := isb.totalInputTokensOrEstimate()
	if totalTokens == 0 {
		return ""
	}

	return fmt.Sprintf("T.%d", totalTokens)
}

// totalInputTokensOrEstimate returns the cumulative TotalInputTokens reported
// by the gateway, or - when the provider did not return usage - falls back
// to estimating tokens from the current message buffer. Drives the cumulative
// T.XXX indicator.
func (isb *InputStatusBar) totalInputTokensOrEstimate() int {
	if isb.conversationRepo == nil {
		return 0
	}

	stats := isb.conversationRepo.GetSessionTokens()
	if stats.TotalInputTokens > 0 {
		return stats.TotalInputTokens
	}

	if isb.tokenEstimator == nil {
		return 0
	}

	messages := isb.conversationRepo.GetMessages()
	if len(messages) == 0 {
		return 0
	}

	sdkMessages := make([]sdk.Message, 0, len(messages))
	for _, entry := range messages {
		sdkMessages = append(sdkMessages, entry.Message)
	}
	return isb.tokenEstimator.EstimateMessagesTokens(sdkMessages)
}

// currentContextTokensOrEstimate returns an approximation of the tokens that
// would be sent in the next request - i.e. how full the model's context window
// is right now. Prefers the gateway-reported LastInputTokens (which includes
// the system prompt and tool definitions, matching what the optimizer and
// session-rollover manager use); falls back to a tokenizer estimate of the
// current message buffer before the first round-trip.
func (isb *InputStatusBar) currentContextTokensOrEstimate() int {
	if isb.conversationRepo == nil {
		return 0
	}

	stats := isb.conversationRepo.GetSessionTokens()
	if stats.LastInputTokens > 0 {
		return stats.LastInputTokens
	}

	if isb.tokenEstimator == nil {
		return 0
	}

	messages := isb.conversationRepo.GetMessages()
	if len(messages) == 0 {
		return 0
	}

	sdkMessages := make([]sdk.Message, 0, len(messages))
	for _, entry := range messages {
		sdkMessages = append(sdkMessages, entry.Message)
	}
	return isb.tokenEstimator.EstimateMessagesTokens(sdkMessages)
}

// buildCostIndicator builds the cost indicator text
func (isb *InputStatusBar) buildCostIndicator() string {
	if isb.conversationRepo == nil {
		return ""
	}

	costStats := isb.conversationRepo.GetSessionCostStats()

	// Don't show if cost is zero or pricing disabled
	if costStats.TotalCost == 0 {
		return ""
	}

	// Format: 💰 $0.0234
	if costStats.TotalCost < 0.01 {
		return fmt.Sprintf("💰 $%.4f", costStats.TotalCost)
	} else if costStats.TotalCost < 1.0 {
		return fmt.Sprintf("💰 $%.3f", costStats.TotalCost)
	} else {
		return fmt.Sprintf("💰 $%.2f", costStats.TotalCost)
	}
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

// InvalidateGitBranchCache clears the git branch cache to force a refresh
func (isb *InputStatusBar) InvalidateGitBranchCache() {
	isb.gitBranchCache = ""
	isb.gitBranchCacheTime = time.Time{}
}

// buildGitBranchLine renders the git branch as its own status bar row, left-aligned
// with the indicator row above it. Returns "" when the git_branch indicator is
// disabled or there is no branch to show (not a repo / detached HEAD), in which case
// the indicators reclaim the full line budget.
func (isb *InputStatusBar) buildGitBranchLine(leftPadding, dimColor string) string {
	if !isb.shouldShowIndicator("git_branch") {
		return ""
	}

	part := isb.buildGitBranchIndicator()
	if part == "" {
		return ""
	}

	return leftPadding + isb.styleProvider.RenderWithColor(part, dimColor)
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

	return fmt.Sprintf("🔧 %d (%d)", count, tokens)
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

// getA2ATaskInfo returns A2A background task count information
func (isb *InputStatusBar) getA2ATaskInfo() string {
	if isb.backgroundTaskService == nil {
		return ""
	}

	tasks := isb.backgroundTaskService.GetBackgroundTasks()
	activeCount := 0
	for _, task := range tasks {
		if task.IsPolling {
			activeCount++
		}
	}

	if activeCount == 0 {
		return ""
	}

	return fmt.Sprintf("Tasks: (%d)", activeCount)
}

// getContextUsageIndicator returns a context usage indicator string.
// Measures how full the model's context window is for the *next* request:
// the gateway-reported LastInputTokens from the most recent call (which
// includes system prompt and tool definitions) divided by the model's
// context window. Falls back to the tokenizer polyfill before the first
// round-trip. Renders HIGH/FULL warning labels at high thresholds.
// This is NOT the cumulative session token count - that's shown by T.XXX.
func (isb *InputStatusBar) getContextUsageIndicator(model string) string {
	contextTokens := isb.currentContextTokensOrEstimate()
	if contextTokens == 0 {
		return ""
	}

	contextWindow := isb.estimateContextWindow(model)
	if contextWindow == 0 {
		return ""
	}

	usagePercent := float64(contextTokens) * 100 / float64(contextWindow)

	displayPercent := usagePercent
	if displayPercent > 100 {
		displayPercent = 100
	}

	switch {
	case usagePercent >= 90:
		return fmt.Sprintf("Context: %.0f%% FULL", displayPercent)
	case usagePercent >= 75:
		return fmt.Sprintf("Context: %.0f%% HIGH", displayPercent)
	default:
		return fmt.Sprintf("Context: %.1f%%", displayPercent)
	}
}

// estimateContextWindow returns an estimated context window size based on model name
func (isb *InputStatusBar) estimateContextWindow(model string) int {
	return models.EstimateContextWindow(model)
}

// Bubble Tea interface
func (isb *InputStatusBar) Init() tea.Cmd { return nil }

func (isb *InputStatusBar) View() tea.View { return tea.NewView(isb.Render()) }

func (isb *InputStatusBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		isb.SetWidth(msg.Width)
	case domain.BashCommandCompletedEvent:
		isb.InvalidateGitBranchCache()
	}
	return isb, nil
}
