package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	models "github.com/inference-gateway/cli/internal/platform/models"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	mcpdomain "github.com/inference-gateway/cli/internal/protocols/mcp/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// InputStatusBar displays input status information like model, theme, agents
// AgentReadiness handles A2A agent readiness tracking
type AgentReadiness interface {
	InitializeAgentReadiness(totalAgents int)
	UpdateAgentStatus(name string, state a2adomain.AgentState, message string, url string, image string)
	SetAgentError(name string, err error)
	GetAgentReadiness() *tui.AgentReadinessState
	AreAllAgentsReady() bool
	ClearAgentReadiness()
	RemoveAgent(name string)
}

type InputStatusBar struct {
	width                  int
	modelService           convdomain.ModelService
	effortSource           effortSource
	themeService           tui.ThemeService
	stateManager           statusBarState
	config                 *config.Config
	conversationRepo       convdomain.ConversationRepository
	toolService            agentdomain.ToolService
	tokenEstimator         convdomain.TokenEstimator
	backgroundShellService scheddomain.BackgroundShellService
	backgroundTaskService  a2adomain.BackgroundTaskService
	messageQueue           convdomain.MessageQueue
	mcpStatus              *mcpdomain.ServerStatus
	browserConnected       bool
	screenRecording        bool
	versionInfo            tui.VersionInfo
	styleProvider          *styles.Provider
	currentInputText       string
	runStartedAt           time.Time
	runConversationID      string

	// Keyboard focus state: when focused, selected indexes the actionable
	// indicators (those that open a view) in build order.
	focused  bool
	selected int
}

// indicatorPart is one status-bar segment plus the view it opens when
// activated (StatusIndicatorActionNone for display-only segments).
type indicatorPart struct {
	text     string
	action   tui.StatusIndicatorAction
	selected bool
	color    string
}

// NewInputStatusBar creates a new input status bar
func NewInputStatusBar(styleProvider *styles.Provider) *InputStatusBar {
	return &InputStatusBar{
		width:         80,
		styleProvider: styleProvider,
	}
}

// SetModelService sets the model service
func (isb *InputStatusBar) SetModelService(modelService convdomain.ModelService) {
	isb.modelService = modelService
}

// effortSource is the narrow slice of AgentService the status bar reads for
// the runtime reasoning effort level.
type effortSource interface {
	GetReasoningEffort() string
}

// SetEffortSource sets the source of the runtime reasoning effort level.
func (isb *InputStatusBar) SetEffortSource(src effortSource) {
	isb.effortSource = src
}

// SetThemeService sets the theme service
func (isb *InputStatusBar) SetThemeService(themeService tui.ThemeService) {
	isb.themeService = themeService
}

// statusBarState is the narrow slice of the state store the input status bar reads.
type statusBarState interface {
	agentdomain.AgentModeState
	AgentReadiness
	GetChatSession() *tui.ChatSession
}

// SetStateManager sets the state manager
func (isb *InputStatusBar) SetStateManager(stateManager statusBarState) {
	isb.stateManager = stateManager
}

// SetConfig sets the config for the status bar
func (isb *InputStatusBar) SetConfig(cfg *config.Config) {
	isb.config = cfg
}

// SetConversationRepo sets the conversation repository
func (isb *InputStatusBar) SetConversationRepo(repo convdomain.ConversationRepository) {
	isb.conversationRepo = repo
}

// SetToolService sets the tool service
func (isb *InputStatusBar) SetToolService(toolService agentdomain.ToolService) {
	isb.toolService = toolService
}

// SetTokenEstimator sets the token estimator
func (isb *InputStatusBar) SetTokenEstimator(estimator convdomain.TokenEstimator) {
	isb.tokenEstimator = estimator
}

// SetBackgroundShellService sets the background shell service
func (isb *InputStatusBar) SetBackgroundShellService(service scheddomain.BackgroundShellService) {
	isb.backgroundShellService = service
}

// SetBackgroundTaskService sets the background task service
func (isb *InputStatusBar) SetBackgroundTaskService(service a2adomain.BackgroundTaskService) {
	isb.backgroundTaskService = service
}

// SetMessageQueue sets the shared message queue so the bar can show what is
// waiting while the agent is busy. Read live at render time, so enqueues and
// drains show up on the next render without extra event plumbing.
func (isb *InputStatusBar) SetMessageQueue(mq convdomain.MessageQueue) {
	isb.messageQueue = mq
}

// UpdateMCPStatus updates the MCP server status (called by event handler)
func (isb *InputStatusBar) UpdateMCPStatus(status *mcpdomain.ServerStatus) {
	isb.mcpStatus = status
}

// SetVersionInfo sets the CLI and gateway versions shown right-aligned in the bar.
func (isb *InputStatusBar) SetVersionInfo(info tui.VersionInfo) {
	isb.versionInfo = info
}

// SetBrowserConnected toggles the browser-extension indicator.
func (isb *InputStatusBar) SetBrowserConnected(connected bool) {
	isb.browserConnected = connected
}

// SetScreenRecording toggles the REC badge shown while a RecordStart screen
// recording runs.
func (isb *InputStatusBar) SetScreenRecording(active bool) {
	isb.screenRecording = active
}

// SetInputText sets the current input text for mode detection
func (isb *InputStatusBar) SetInputText(text string) {
	isb.currentInputText = text
}

func (isb *InputStatusBar) SetWidth(width int) {
	isb.width = width
}

func (isb *InputStatusBar) SetHeight(height int) {
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
func (isb *InputStatusBar) SelectedAction() tui.StatusIndicatorAction {
	actions := isb.actionableActions()
	if len(actions) == 0 {
		return tui.StatusIndicatorActionNone
	}
	isb.clampSelection(len(actions))
	return actions[isb.selected]
}

// actionableActions lists the actions of the currently visible indicators
// that open a view, in build order.
func (isb *InputStatusBar) actionableActions() []tui.StatusIndicatorAction {
	var actions []tui.StatusIndicatorAction
	for _, part := range isb.getAllIndicatorParts() {
		if part.action != tui.StatusIndicatorActionNone {
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
	if right := isb.renderRightSegment(lipgloss.Width(lines[0])); right != "" {
		lines[0] += right
	}
	return strings.Join(lines, "\n")
}

// versionRightInset is the trailing gap the right-aligned version segment
// keeps from the window edge. Shared with SubagentList, whose duration
// column ends on the same edge instead of passing it.
const versionRightInset = 5

// renderRightSegment right-aligns "● REC • cli vX • gw vY • ● Browser" after
// the given line width, dropping pieces until the rest fits: gateway version
// first, then CLI version, then the Browser label (bare dot last); the REC
// badge stays. Empty when nothing fits.
func (isb *InputStatusBar) renderRightSegment(lineWidth int) string {
	if isb.styleProvider == nil {
		return ""
	}
	dim := func(s string) string {
		return isb.styleProvider.RenderWithColor(s, isb.styleProvider.GetThemeColor("dim"))
	}

	var cli, gw string
	if v := isb.versionInfo.Version; v != "" {
		cli = dim("cli " + versionLabel(v))
	}
	if v := isb.versionInfo.GatewayVersion; v != "" {
		gw = dim("gw " + versionLabel(v))
	}
	dot := isb.buildBridgeDot()
	browser := dot
	if dot != "" {
		browser += dim(" Browser")
	}

	join := func(parts ...string) string {
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, dim(" • "))
	}

	var rec string
	if isb.screenRecording {
		rec = isb.styleProvider.RenderWithColor("● REC", isb.styleProvider.GetThemeColor("error"))
	}

	for _, candidate := range []string{join(rec, cli, gw, browser), join(rec, cli, browser), join(rec, browser), join(rec, dot), rec} {
		if candidate == "" {
			continue
		}
		if pad := isb.width - versionRightInset - lineWidth - lipgloss.Width(candidate); pad >= 2 {
			return strings.Repeat(" ", pad) + candidate
		}
	}
	return ""
}

// versionLabel prefixes a numeric version with "v"; non-numeric values
// (e.g. "dev") pass through unchanged.
func versionLabel(v string) string {
	if v == "" || v[0] < '0' || v[0] > '9' {
		return v
	}
	return "v" + v
}

// buildBridgeDot is the extension-bridge marker: green when the extension is
// connected, yellow while the bridge waits for one, nothing when the extension
// backend is not configured.
func (isb *InputStatusBar) buildBridgeDot() string {
	if isb.config == nil || isb.styleProvider == nil || !isb.config.BrowserUse.Enabled ||
		isb.config.BrowserUse.Backend != config.BrowserBackendExtension {
		return ""
	}
	color := isb.styleProvider.GetThemeColor("warning")
	if isb.browserConnected {
		color = isb.styleProvider.GetThemeColor("success")
	}
	return isb.styleProvider.RenderWithColor("●", color)
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
		if part.action != tui.StatusIndicatorActionNone {
			actionable++
		}
	}
	if actionable == 0 {
		return marked
	}
	isb.clampSelection(actionable)

	ordinal := 0
	for i := range marked {
		if marked[i].action == tui.StatusIndicatorActionNone {
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

	hasColor := false
	for _, part := range parts {
		if part.color != "" {
			hasColor = true
			break
		}
	}

	if !isb.focused && !hasColor {
		return isb.styleProvider.RenderWithColor(strings.Join(texts, " • "), dimColor)
	}

	styled := make([]string, len(parts))
	for i, part := range parts {
		switch {
		case part.selected:
			styled[i] = isb.styleProvider.RenderSelectedIndicator(texts[i])
		case part.color != "":
			styled[i] = isb.styleProvider.RenderWithColor(texts[i], part.color)
		default:
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
//
//nolint:gocyclo,cyclop // one gated block per indicator, in build order
func (isb *InputStatusBar) buildIndicatorParts(currentModel string) []indicatorPart {
	parts := []indicatorPart{}

	if isb.shouldShowIndicator("model") {
		parts = append(parts, indicatorPart{text: currentModel, action: tui.StatusIndicatorActionModelSelection})
	}

	if isb.shouldShowIndicator("effort") {
		if effortPart := isb.buildEffortIndicator(); effortPart != "" {
			parts = append(parts, indicatorPart{text: effortPart})
		}
	}

	if isb.shouldShowIndicator("theme") {
		if themePart := isb.buildThemeIndicator(); themePart != "" {
			parts = append(parts, indicatorPart{text: themePart, action: tui.StatusIndicatorActionThemeSelection})
		}
	}

	if isb.shouldShowIndicator("max_output") {
		if maxOutputPart := isb.buildMaxOutputIndicator(); maxOutputPart != "" {
			parts = append(parts, indicatorPart{text: maxOutputPart})
		}
	}

	if isb.shouldShowIndicator("a2a_agents") {
		if agentsPart := isb.buildA2AAgentsIndicator(); agentsPart != "" {
			parts = append(parts, indicatorPart{text: agentsPart, action: tui.StatusIndicatorActionA2AAgents, color: isb.a2aIndicatorColor()})
		}
	}

	if isb.shouldShowIndicator("tools") {
		if toolInfo := isb.getToolInfo(); toolInfo != "" {
			parts = append(parts, indicatorPart{text: toolInfo, action: tui.StatusIndicatorActionToolsList})
		}
	}

	if isb.shouldShowIndicator("queue") {
		if queuePart := isb.buildQueueIndicator(); queuePart != "" {
			var color string
			if isb.styleProvider != nil {
				color = isb.styleProvider.GetThemeColor("accent")
			}
			parts = append(parts, indicatorPart{text: queuePart, color: color})
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
		if cachedTokensPart := isb.buildCachedTokensIndicator(); cachedTokensPart != "" {
			parts = append(parts, indicatorPart{text: cachedTokensPart})
		}
	}

	if isb.shouldShowIndicator("cost") {
		if costPart := isb.buildCostIndicator(); costPart != "" {
			parts = append(parts, indicatorPart{text: costPart})
		}
	}

	if isb.shouldShowIndicator("duration") {
		if durationPart := isb.buildDurationIndicator(); durationPart != "" {
			parts = append(parts, indicatorPart{text: durationPart})
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

	if isb.shouldShowIndicator("effort") {
		if effortPart := isb.buildEffortIndicator(); effortPart != "" {
			parts = append(parts, effortPart)
		}
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
	case "effort":
		return indicators.Effort
	case "theme":
		return indicators.Theme
	case "max_output":
		return indicators.MaxOutput
	case "a2a_agents":
		return indicators.A2AAgents
	case "tools":
		return indicators.Tools
	case "queue":
		return indicators.Queue
	case "mcp":
		return indicators.MCP
	case "context_usage":
		return indicators.ContextUsage
	case "session_tokens":
		return indicators.SessionTokens
	case "cost":
		return indicators.Cost
	case "duration":
		return indicators.Duration
	case "git_branch":
		return indicators.GitBranch
	case "git_pr":
		return indicators.GitPR
	default:
		return true
	}
}

// buildEffortIndicator shows the reasoning effort level in effect. Anthropic
// models only - other providers don't support the effort switch. When nothing
// is set, the CLI's hardcoded Anthropic default is what goes on the wire, so
// that's what shows.
func (isb *InputStatusBar) buildEffortIndicator() string {
	if isb.effortSource == nil || isb.modelService == nil {
		return ""
	}
	if !strings.HasPrefix(isb.modelService.GetCurrentModel(), string(sdk.Anthropic)+"/") {
		return ""
	}
	effort := isb.effortSource.GetReasoningEffort()
	if effort == "" {
		effort = config.DefaultAnthropicEffort
	}
	return fmt.Sprintf("Effort: %s", effort)
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

// a2aIndicatorColor color-codes the A2A segment: green once all agents are
// ready, red when any agent failed, empty (dim) while starting up.
func (isb *InputStatusBar) a2aIndicatorColor() string {
	if isb.stateManager == nil || isb.styleProvider == nil {
		return ""
	}
	readiness := isb.stateManager.GetAgentReadiness()
	if readiness == nil || readiness.TotalAgents == 0 {
		return ""
	}
	if readiness.ReadyAgents >= readiness.TotalAgents {
		return isb.styleProvider.GetThemeColor("success")
	}
	for _, agent := range readiness.Agents {
		if agent.State == a2adomain.AgentStateFailed {
			return isb.styleProvider.GetThemeColor("error")
		}
	}
	return ""
}

// buildMCPIndicator builds the MCP server status indicator text
func (isb *InputStatusBar) buildMCPIndicator() string {
	if isb.mcpStatus == nil || isb.config == nil || len(isb.config.MCP.Servers) == 0 {
		return ""
	}
	if isb.mcpStatus.TotalTools > 0 {
		return fmt.Sprintf("MCP %d/%d (%d)", isb.mcpStatus.ConnectedServers, isb.mcpStatus.TotalServers, isb.mcpStatus.TotalTools)
	}
	return fmt.Sprintf("MCP %d/%d", isb.mcpStatus.ConnectedServers, isb.mcpStatus.TotalServers)
}

// buildSessionTokensIndicator builds the session token indicator. The leading
// figure is the size of the next request - the same context figure the Context
// indicator divides by the model's window - and T. is the cumulative input
// tokens billed across the whole session (the number that drives the cost).
// Both fall back to a tokenizer estimate when the provider returned no usage.
func (isb *InputStatusBar) buildSessionTokensIndicator() string {
	if isb.conversationRepo == nil {
		return ""
	}

	current := isb.currentContextTokensOrEstimate()
	total := isb.totalInputTokensOrEstimate()
	if current == 0 && total == 0 {
		return ""
	}

	return fmt.Sprintf("%s T.%s", compactCount(current), compactCount(total))
}

// buildCachedTokensIndicator builds the cumulative cached-prompt-tokens
// indicator (provider-reported prompt cache hits). Hidden while zero -
// providers without prompt caching, or usage polyfilled by the tokenizer,
// never report cached tokens.
func (isb *InputStatusBar) buildCachedTokensIndicator() string {
	if isb.conversationRepo == nil {
		return ""
	}

	cached := isb.conversationRepo.GetSessionTokens().TotalCachedTokens
	if cached == 0 {
		return ""
	}

	return fmt.Sprintf("C.%s", compactCount(cached))
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
// is right now. Takes the larger of the gateway-reported LastInputTokens (which
// includes the system prompt and tool definitions, matching what the optimizer
// and session-rollover manager use) and a fresh tokenizer estimate of the
// current buffer, so a single-turn tool-output spike shows up immediately
// instead of lagging behind the last round-trip.
func (isb *InputStatusBar) currentContextTokensOrEstimate() int {
	if isb.conversationRepo == nil {
		return 0
	}

	stats := isb.conversationRepo.GetSessionTokens()

	if isb.tokenEstimator == nil {
		return stats.LastInputTokens
	}

	messages := isb.conversationRepo.GetMessages()
	if len(messages) == 0 {
		return stats.LastInputTokens
	}

	sdkMessages := make([]sdk.Message, 0, len(messages))
	for _, entry := range messages {
		sdkMessages = append(sdkMessages, entry.Message)
	}
	return isb.tokenEstimator.EffectiveContextTokens(stats.LastInputTokens, sdkMessages)
}

// buildCostIndicator builds the cost indicator text
func (isb *InputStatusBar) buildCostIndicator() string {
	if isb.conversationRepo == nil {
		return ""
	}

	costStats := isb.conversationRepo.GetSessionCostStats()

	if costStats.TotalCost == 0 {
		return ""
	}

	if costStats.TotalCost < 0.01 {
		return fmt.Sprintf("$%.4f", costStats.TotalCost)
	} else if costStats.TotalCost < 1.0 {
		return fmt.Sprintf("$%.3f", costStats.TotalCost)
	} else {
		return fmt.Sprintf("$%.2f", costStats.TotalCost)
	}
}

// buildDurationIndicator shows how long the agent has worked in this session:
// a stopwatch that runs while a run is in flight, holds at a terminal state
// and resumes with the next run. Empty before the session's first run.
func (isb *InputStatusBar) buildDurationIndicator() string {
	if isb.conversationRepo == nil {
		return ""
	}
	total := isb.conversationRepo.GetActiveDuration()
	if isb.timingCurrentSession() {
		total += time.Since(isb.runStartedAt)
	}
	if total == 0 {
		return ""
	}
	return formatDuration(total)
}

// timingCurrentSession reports whether a run is being timed and still belongs
// to the session on screen. A session cleared or swapped mid-run drops it.
func (isb *InputStatusBar) timingCurrentSession() bool {
	return !isb.runStartedAt.IsZero() &&
		isb.conversationRepo != nil &&
		isb.conversationRepo.GetCurrentConversationID() == isb.runConversationID &&
		isb.conversationRepo.GetMessageCount() > 0
}

// trackRun starts the session stopwatch on the first turn of a run and stops
// it at a terminal state: an error, a cancel or a final answer with no tool
// calls. A turn with tool calls feeds back in, so the stopwatch keeps running.
// An interrupt ends the chat session before its cancel event arrives, so a
// missing session stops the stopwatch too.
func (isb *InputStatusBar) trackRun(msg tea.Msg) tea.Cmd {
	if isb.stateManager != nil && isb.stateManager.GetChatSession() == nil {
		return isb.stopRun()
	}
	switch msg := msg.(type) {
	case agentdomain.ChatStartEvent:
		if isb.runStartedAt.IsZero() && isb.conversationRepo != nil {
			isb.runStartedAt = time.Now()
			isb.runConversationID = isb.conversationRepo.GetCurrentConversationID()
		}
	case agentdomain.ChatCompleteEvent:
		if msg.Cancelled || len(msg.ToolCalls) == 0 {
			return isb.stopRun()
		}
	case agentdomain.ChatErrorEvent:
		return isb.stopRun()
	}
	return nil
}

// stopRun banks the run's working time on the session it was timed against
// and returns the command that persists it, since a run ends after its last
// message and nothing else would save the new total.
func (isb *InputStatusBar) stopRun() tea.Cmd {
	if isb.runStartedAt.IsZero() {
		return nil
	}
	banked := isb.timingCurrentSession()
	if banked {
		_ = isb.conversationRepo.AddActiveDuration(time.Since(isb.runStartedAt))
	}
	isb.runStartedAt = time.Time{}

	saver, persistent := isb.conversationRepo.(conversationSaver)
	if !banked || !persistent {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sessionSaveTimeout)
		defer cancel()
		if err := saver.SaveConversation(ctx); err != nil {
			logger.Debug("failed to save the session working time", "error", err)
		}
		return nil
	}
}

// conversationSaver is the persistence a storage-backed repository adds.
type conversationSaver interface {
	SaveConversation(ctx context.Context) error
}

const sessionSaveTimeout = 30 * time.Second

// getToolInfo returns tool count and token information
func (isb *InputStatusBar) getToolInfo() string {
	if isb.toolService == nil || isb.tokenEstimator == nil {
		return ""
	}

	agentMode := agentdomain.AgentModeStandard
	if isb.stateManager != nil {
		agentMode = isb.stateManager.GetAgentMode()
	}

	tokens, count := isb.tokenEstimator.GetToolStats(isb.toolService, agentMode)
	if count == 0 {
		return ""
	}

	return fmt.Sprintf("Tools: %d (%d)", count, tokens)
}

// buildQueueIndicator counts the messages waiting in the shared queue while
// the agent is busy. Empty while the queue is empty.
func (isb *InputStatusBar) buildQueueIndicator() string {
	if isb.messageQueue == nil || isb.messageQueue.IsEmpty() {
		return ""
	}
	return fmt.Sprintf("%s %d queued", icons.QueueIcon, isb.messageQueue.Size())
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

	contextWindow, _ := models.LookupContextWindow(model)
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

// Bubble Tea interface
func (isb *InputStatusBar) Init() tea.Cmd { return nil }

func (isb *InputStatusBar) View() tea.View { return tea.NewView(isb.Render()) }

func (isb *InputStatusBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		isb.SetWidth(windowMsg.Width)
	}
	return isb, isb.trackRun(msg)
}
