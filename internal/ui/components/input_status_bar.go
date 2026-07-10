package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	ui "github.com/inference-gateway/cli/internal/ui"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
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
	backgroundTaskRegistry domain.BackgroundTaskRegistry
	mcpStatus              *domain.MCPServerStatus
	styleProvider          *styles.Provider
	currentInputText       string

	// Keyboard focus state: when focused, selected indexes the actionable
	// indicators (those that open a view) in build order.
	focused  bool
	selected int
}

// indicatorPart is one status-bar segment plus the view it opens when
// activated (StatusIndicatorActionNone for display-only segments).
type indicatorPart struct {
	text     string
	action   ui.StatusIndicatorAction
	selected bool
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

// SetBackgroundTaskRegistry sets the unified background task registry, the single
// source for the live A2A/shell/subagent counts shown in the status line.
func (isb *InputStatusBar) SetBackgroundTaskRegistry(registry domain.BackgroundTaskRegistry) {
	isb.backgroundTaskRegistry = registry
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

// Focus moves keyboard focus onto the indicator row, selecting the first
// actionable indicator. Reports false when nothing is actionable so the
// caller can keep focus in the input.
func (isb *InputStatusBar) Focus() bool {
	if len(isb.actionableActions()) == 0 {
		return false
	}
	isb.focused = true
	isb.selected = 0
	return true
}

// Blur returns the indicator row to its passive display-only state.
func (isb *InputStatusBar) Blur() {
	isb.focused = false
}

// IsFocused reports whether the indicator row holds keyboard focus.
func (isb *InputStatusBar) IsFocused() bool {
	return isb.focused
}

// SelectNext moves the selection to the next actionable indicator, wrapping.
func (isb *InputStatusBar) SelectNext() {
	if count := len(isb.actionableActions()); count > 0 {
		isb.clampSelection(count)
		isb.selected = (isb.selected + 1) % count
	}
}

// SelectPrev moves the selection to the previous actionable indicator, wrapping.
func (isb *InputStatusBar) SelectPrev() {
	if count := len(isb.actionableActions()); count > 0 {
		isb.clampSelection(count)
		isb.selected = (isb.selected - 1 + count) % count
	}
}

// SelectedAction returns the action of the selected indicator, clamping the
// selection when indicators disappeared since it was set (e.g. jobs finished).
func (isb *InputStatusBar) SelectedAction() ui.StatusIndicatorAction {
	actions := isb.actionableActions()
	if len(actions) == 0 {
		return ui.StatusIndicatorActionNone
	}
	isb.clampSelection(len(actions))
	return actions[isb.selected]
}

// actionableActions lists the actions of the currently visible indicators
// that open a view, in build order.
func (isb *InputStatusBar) actionableActions() []ui.StatusIndicatorAction {
	var actions []ui.StatusIndicatorAction
	for _, part := range isb.getAllIndicatorParts() {
		if part.action != ui.StatusIndicatorActionNone {
			actions = append(actions, part.action)
		}
	}
	return actions
}

func (isb *InputStatusBar) clampSelection(count int) {
	if isb.selected >= count {
		isb.selected = count - 1
	}
	if isb.selected < 0 {
		isb.selected = 0
	}
}

func (isb *InputStatusBar) Render() string {
	if isb.config != nil && !isb.config.Chat.StatusBar.Enabled {
		return ""
	}

	lines := isb.buildStatusLines()
	return strings.Join(lines, "\n")
}

// buildStatusLines builds the status bar content. Indicators are packed onto up
// to maxLines rows; overflow beyond that is collapsed with an ellipsis. The git
// branch is rendered separately, in the input box top border (see InputView), so
// it never competes with these indicators for horizontal space.
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

	parts := isb.getAllIndicatorParts()
	if len(parts) == 0 {
		return []string{leftPadding + "\u00A0"}
	}

	if isb.focused {
		parts = isb.markSelected(parts)
	}

	lineGroups := isb.splitPartsIntoLines(parts, availableWidth, maxLines, separatorWidth)
	lineGroups = capIndicatorLines(lineGroups, maxLines)

	var lines []string
	for _, lineItems := range lineGroups {
		lines = append(lines, leftPadding+isb.renderIndicatorLine(lineItems, dimColor))
	}

	if len(lines) == 0 {
		return []string{leftPadding + "\u00A0"}
	}

	return lines
}

// markSelected returns a copy of parts with the selected actionable part
// flagged for highlighting.
func (isb *InputStatusBar) markSelected(parts []indicatorPart) []indicatorPart {
	marked := make([]indicatorPart, len(parts))
	copy(marked, parts)

	actionable := 0
	for _, part := range marked {
		if part.action != ui.StatusIndicatorActionNone {
			actionable++
		}
	}
	if actionable == 0 {
		return marked
	}
	isb.clampSelection(actionable)

	ordinal := 0
	for i := range marked {
		if marked[i].action == ui.StatusIndicatorActionNone {
			continue
		}
		if ordinal == isb.selected {
			marked[i].selected = true
			break
		}
		ordinal++
	}
	return marked
}

// renderIndicatorLine styles one row of indicators. Unfocused rows keep the
// single dim style; focused rows style per part so the selected one stands
// out on a background highlight.
func (isb *InputStatusBar) renderIndicatorLine(parts []indicatorPart, dimColor string) string {
	texts := make([]string, len(parts))
	for i, part := range parts {
		texts[i] = part.text
	}

	if !isb.focused {
		return isb.styleProvider.RenderWithColor(strings.Join(texts, " • "), dimColor)
	}

	styled := make([]string, len(parts))
	for i, part := range parts {
		if part.selected {
			styled[i] = isb.styleProvider.RenderSelectedIndicator(texts[i])
		} else {
			styled[i] = isb.styleProvider.RenderWithColor(texts[i], dimColor)
		}
	}
	return strings.Join(styled, isb.styleProvider.RenderWithColor(" • ", dimColor))
}

// getAllIndicatorParts returns all indicator parts as a slice
func (isb *InputStatusBar) getAllIndicatorParts() []indicatorPart {
	if isb.modelService == nil {
		return nil
	}

	currentModel := isb.modelService.GetCurrentModel()
	if currentModel == "" {
		return nil
	}

	return isb.buildIndicatorParts(currentModel)
}

// buildIndicatorParts builds individual indicator parts without joining them.
// The git branch is not included here - it is rendered in the input box top
// border by InputView, not in the status bar.
func (isb *InputStatusBar) buildIndicatorParts(currentModel string) []indicatorPart {
	parts := []indicatorPart{}

	if isb.shouldShowIndicator("model") {
		parts = append(parts, indicatorPart{text: currentModel, action: ui.StatusIndicatorActionModelSelection})
	}

	if isb.shouldShowIndicator("theme") {
		if themePart := isb.buildThemeIndicator(); themePart != "" {
			parts = append(parts, indicatorPart{text: themePart, action: ui.StatusIndicatorActionThemeSelection})
		}
	}

	if isb.shouldShowIndicator("max_output") {
		if maxOutputPart := isb.buildMaxOutputIndicator(); maxOutputPart != "" {
			parts = append(parts, indicatorPart{text: maxOutputPart})
		}
	}

	if isb.shouldShowIndicator("a2a_agents") {
		if agentsPart := isb.buildA2AAgentsIndicator(); agentsPart != "" {
			parts = append(parts, indicatorPart{text: agentsPart, action: ui.StatusIndicatorActionA2AAgents})
		}
	}

	if isb.shouldShowIndicator("tools") {
		if toolInfo := isb.getToolInfo(); toolInfo != "" {
			parts = append(parts, indicatorPart{text: toolInfo, action: ui.StatusIndicatorActionToolsList})
		}
	}

	if isb.shouldShowIndicator("background_shells") || isb.shouldShowIndicator("a2a_tasks") {
		if jobsInfo := isb.getBackgroundJobsInfo(); jobsInfo != "" {
			parts = append(parts, indicatorPart{text: jobsInfo, action: ui.StatusIndicatorActionTaskManagement})
		}
	}

	if isb.shouldShowIndicator("mcp") {
		if mcpPart := isb.buildMCPIndicator(); mcpPart != "" {
			parts = append(parts, indicatorPart{text: mcpPart})
		}
	}

	if isb.shouldShowIndicator("context_usage") {
		if contextIndicator := isb.getContextUsageIndicator(currentModel); contextIndicator != "" {
			parts = append(parts, indicatorPart{text: contextIndicator})
		}
	}

	if isb.shouldShowIndicator("session_tokens") {
		if sessionTokensPart := isb.buildSessionTokensIndicator(); sessionTokensPart != "" {
			parts = append(parts, indicatorPart{text: sessionTokensPart})
		}
	}

	if isb.shouldShowIndicator("cost") {
		if costPart := isb.buildCostIndicator(); costPart != "" {
			parts = append(parts, indicatorPart{text: costPart})
		}
	}

	return parts
}

// selectedIndicatorPadding is the extra width the selected part's pill adds:
// one column of padding on each side (see Provider.RenderSelectedIndicator).
const selectedIndicatorPadding = 2

// splitPartsIntoLines splits indicator parts into line groups based on available width
func (isb *InputStatusBar) splitPartsIntoLines(parts []indicatorPart, availableWidth, maxLines, separatorWidth int) [][]indicatorPart {
	var lineGroups [][]indicatorPart
	currentLineItems := []indicatorPart{}
	currentLineWidth := 0

	for i, part := range parts {
		itemWidth := len(part.text)
		if part.selected {
			itemWidth += selectedIndicatorPadding
		}
		separatorLen := 0

		if len(currentLineItems) > 0 {
			separatorLen = separatorWidth
		}

		needsNewLine := len(currentLineItems) > 0 && currentLineWidth+separatorLen+itemWidth > availableWidth
		if needsNewLine {
			lineGroups = append(lineGroups, currentLineItems)
			currentLineItems = []indicatorPart{part}
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
func capIndicatorLines(lineGroups [][]indicatorPart, maxLines int) [][]indicatorPart {
	if maxLines <= 0 {
		return nil
	}
	if len(lineGroups) <= maxLines {
		return lineGroups
	}

	capped := lineGroups[:maxLines]
	last := capped[maxLines-1]
	if n := len(last); n == 0 || (last[n-1].text != "…" && last[n-1].text != "...") {
		capped[maxLines-1] = append(last, indicatorPart{text: "…"})
	}
	return capped
}

// shouldAddOverflowAndBreak checks if we've reached max lines and adds overflow indicator if needed
func (isb *InputStatusBar) shouldAddOverflowAndBreak(currentLines, maxLines, currentIndex, totalParts, lineWidth, separatorWidth, availableWidth int, lineItems *[]indicatorPart) bool {
	if currentLines < maxLines {
		return false
	}

	if currentIndex < totalParts-1 {
		overflowWidth := 3
		if lineWidth+separatorWidth+overflowWidth <= availableWidth {
			*lineItems = append(*lineItems, indicatorPart{text: "..."})
		}
	}

	return true
}

func (isb *InputStatusBar) buildModelDisplayText(currentModel string) string {
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

	if isb.shouldShowIndicator("background_shells") || isb.shouldShowIndicator("a2a_tasks") {
		if jobsInfo := isb.getBackgroundJobsInfo(); jobsInfo != "" {
			parts = append(parts, jobsInfo)
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
	case "git_pr":
		return indicators.GitPR
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
		return fmt.Sprintf("A2A: %d/%d", readiness.ReadyAgents, readiness.TotalAgents)
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

	return fmt.Sprintf("Tools: %d (%d)", count, tokens)
}

// getBackgroundInfo returns background process count information
func (isb *InputStatusBar) getBackgroundJobsInfo() string {
	if isb.backgroundTaskRegistry == nil {
		return ""
	}

	a2a := isb.backgroundTaskRegistry.CountRunningJobs(domain.JobKindA2A)
	shells := isb.backgroundTaskRegistry.CountRunningJobs(domain.JobKindShell)
	subagents := isb.backgroundTaskRegistry.CountRunningJobs(domain.JobKindSubagent)

	var segments []string
	if a2a > 0 {
		segments = append(segments, fmt.Sprintf("%d A2A", a2a))
	}
	if shells > 0 {
		segments = append(segments, fmt.Sprintf("%d shells", shells))
	}
	if subagents > 0 {
		segments = append(segments, fmt.Sprintf("%d subagents", subagents))
	}
	if len(segments) == 0 {
		return ""
	}
	return "⚙ " + strings.Join(segments, " · ")
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

	contextWindow, known := models.LookupContextWindow(model)
	if !known || contextWindow == 0 {
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

// Bubble Tea interface
func (isb *InputStatusBar) Init() tea.Cmd { return nil }

func (isb *InputStatusBar) View() tea.View { return tea.NewView(isb.Render()) }

func (isb *InputStatusBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		isb.SetWidth(windowMsg.Width)
	}
	return isb, nil
}
