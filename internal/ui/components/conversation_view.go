package components

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	spinner "charm.land/bubbles/v2/spinner"
	viewport "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	hints "github.com/inference-gateway/cli/internal/ui/hints"
	markdown "github.com/inference-gateway/cli/internal/ui/markdown"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// NavigationMode represents the current navigation state of the conversation view
type NavigationMode int

const (
	// NavigationModeNormal is the default mode for displaying conversation
	NavigationModeNormal NavigationMode = iota
	// NavigationModeMessageHistory is the mode for navigating message history
	NavigationModeMessageHistory
)

// backgroundTaskRemovalDelay is how long a terminal-state background-task
// indicator lingers under the originating tool call before auto-removal.
const backgroundTaskRemovalDelay = 5 * time.Second

// BackgroundTaskDisplay tracks the live state of a remote A2A task for
// inline visualisation under the originating A2A_SubmitTask tool result.
// It is UI-only ephemeral state and is not persisted with the conversation.
type BackgroundTaskDisplay struct {
	TaskID             string
	AgentName          string
	AgentURL           string
	Model              string
	State              string
	Message            string
	UsageJSON          string
	ExecutionStatsJSON string
	ErrorMsg           string
	IsTerminal         bool
	StartedAt          time.Time
	CompletedAt        time.Time
}

// BackgroundTaskRemovalTickMsg is dispatched backgroundTaskRemovalDelay after a
// task reaches a terminal state to remove its inline indicator from the view.
type BackgroundTaskRemovalTickMsg struct {
	TaskID string
}

// subagentDisplay tracks the live state of one local subagent (spawned by the
// Agent tool) for the inline tree rendered in the sticky indicator bar. UI-only.
type subagentDisplay struct {
	ID          string
	Label       string
	Status      string // "running" | "done" | "failed"
	StartedAt   time.Time
	CompletedAt time.Time
	IsTerminal  bool
}

// subagentRemovalTickMsg removes a terminal subagent's row from the tree after a delay.
type subagentRemovalTickMsg struct {
	ID string
}

// ConversationView handles the chat conversation display
type ConversationView struct {
	conversation           []domain.ConversationEntry
	Viewport               viewport.Model
	width                  int
	height                 int
	expandedToolResults    map[int]bool
	expandedThinkingBlocks map[int]bool
	allToolsExpanded       bool
	allThinkingExpanded    bool
	defaultExpandedTools   map[string]bool
	toolFormatter          domain.ToolFormatter
	lineFormatter          *formatting.ConversationLineFormatter
	plainTextLines         []string
	configPath             string
	versionInfo            *domain.VersionInfo
	styleProvider          *styles.Provider
	toolCallRenderer       *ToolCallRenderer
	markdownRenderer       *markdown.Renderer
	rawFormat              bool
	userScrolledUp         bool
	stateManager           domain.StateManager
	renderedContent        string

	// renderCache memoizes per-entry rendered output keyed by conversation
	// index; an entry re-renders only when its fingerprint changes. Cleared
	// on theme refresh, which restyles without touching entry state.
	renderCache map[int]renderCacheEntry

	// Streaming state with mutex protection
	streamingMu              sync.RWMutex
	streamingBuffer          strings.Builder
	streamingReasoningBuffer strings.Builder
	isStreaming              bool
	streamingModel           string

	// Viewport mutex to protect concurrent access
	viewportMu sync.Mutex

	keyHintFormatter *hints.Formatter

	// Message history navigation
	navigationMode       NavigationMode
	messageSnapshots     []domain.MessageSnapshot
	historySelectedIndex int

	// Inline background-task indicators for A2A_SubmitTask delegations.
	// Keyed by remote task ID. Entries are inserted on
	// A2ATaskSubmittedEvent, updated on status/complete/fail events, and
	// removed by BackgroundTaskRemovalTickMsg ~5s after a terminal state.
	backgroundTasks    map[string]*BackgroundTaskDisplay
	subagentTasks      map[string]*subagentDisplay
	backgroundSpinStep int
	backgroundSpinner  spinner.Model
	// agentNameResolver maps an agent URL to its configured friendly name
	// from ~/.infer/agents.yaml. Optional; falls back to the URL when nil
	// or when the URL has no matching entry.
	agentNameResolver func(url string) string
	// agentModelResolver maps an agent URL to its configured model (e.g.
	// "deepseek/deepseek-v4-flash") from ~/.infer/agents.yaml. Optional;
	// when nil or no match, the model segment is omitted from the indicator.
	agentModelResolver func(url string) string
}

func NewConversationView(styleProvider *styles.Provider) *ConversationView {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent("")
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	var mdRenderer *markdown.Renderer
	if themeService := styleProvider.GetThemeService(); themeService != nil {
		mdRenderer = markdown.NewRenderer(themeService, 80)
	}

	bgSpin := spinner.New()
	bgSpinStyle := spinner.Dot
	bgSpinStyle.FPS = 100 * time.Millisecond
	bgSpin.Spinner = bgSpinStyle

	return &ConversationView{
		conversation:           []domain.ConversationEntry{},
		Viewport:               vp,
		width:                  80,
		height:                 20,
		expandedToolResults:    make(map[int]bool),
		expandedThinkingBlocks: make(map[int]bool),
		allToolsExpanded:       false,
		allThinkingExpanded:    false,
		defaultExpandedTools:   map[string]bool{"Edit": true, "MultiEdit": true},
		lineFormatter:          formatting.NewConversationLineFormatter(80, nil),
		plainTextLines:         []string{},
		styleProvider:          styleProvider,
		markdownRenderer:       mdRenderer,
		backgroundTasks:        make(map[string]*BackgroundTaskDisplay),
		subagentTasks:          make(map[string]*subagentDisplay),
		backgroundSpinner:      bgSpin,
		renderCache:            make(map[int]renderCacheEntry),
	}
}

// SetToolFormatter sets the tool formatter for this conversation view
func (cv *ConversationView) SetToolFormatter(formatter domain.ToolFormatter) {
	cv.toolFormatter = formatter
	cv.lineFormatter = formatting.NewConversationLineFormatter(cv.width, formatter)
}

// SetConfigPath sets the config path for the welcome message
func (cv *ConversationView) SetConfigPath(configPath string) {
	cv.configPath = configPath
}

// SetVersionInfo sets the version information for the welcome message
func (cv *ConversationView) SetVersionInfo(info domain.VersionInfo) {
	cv.versionInfo = &info
}

// SetToolCallRenderer sets the tool call renderer for displaying real-time tool execution status
func (cv *ConversationView) SetToolCallRenderer(renderer *ToolCallRenderer) {
	cv.toolCallRenderer = renderer
}

// SetStateManager sets the state manager for accessing plan approval state
func (cv *ConversationView) SetStateManager(stateManager domain.StateManager) {
	cv.stateManager = stateManager
}

// SetKeyHintFormatter sets the key hint formatter for displaying keybinding hints
func (cv *ConversationView) SetKeyHintFormatter(formatter *hints.Formatter) {
	cv.keyHintFormatter = formatter
}

// SetAgentNameResolver injects a URL→friendly-name lookup used when
// rendering background-agent indicators. Pass nil to disable resolution
// (the visual then falls back to the agent URL).
func (cv *ConversationView) SetAgentNameResolver(resolver func(url string) string) {
	cv.agentNameResolver = resolver
}

// SetAgentModelResolver injects a URL→model lookup used when rendering
// the background-agent indicator's "model=" segment. Pass nil to omit
// that segment (the visual then drops "model=" cleanly).
func (cv *ConversationView) SetAgentModelResolver(resolver func(url string) string) {
	cv.agentModelResolver = resolver
}

func (cv *ConversationView) SetConversation(conversation []domain.ConversationEntry) {
	wasAtBottom := cv.Viewport.AtBottom()
	if len(conversation) < len(cv.conversation) {
		cv.renderCache = make(map[int]renderCacheEntry)
	}
	cv.conversation = conversation
	cv.updatePlainTextLines()

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContentFull()
		if wasAtBottom {
			cv.Viewport.GotoBottom()
		}
	}
}

func (cv *ConversationView) GetScrollOffset() int {
	return cv.Viewport.YOffset()
}

func (cv *ConversationView) CanScrollUp() bool {
	return !cv.Viewport.AtTop()
}

func (cv *ConversationView) CanScrollDown() bool {
	return !cv.Viewport.AtBottom()
}

// ResetUserScroll resets the user scroll state, enabling auto-scroll to bottom.
// Call this when a new message is sent to ensure the user sees the latest response.
func (cv *ConversationView) ResetUserScroll() {
	cv.userScrolledUp = false
}

func (cv *ConversationView) ToggleToolResultExpansion(index int) {
	if index >= 0 && index < len(cv.conversation) {
		cv.expandedToolResults[index] = !cv.IsToolResultExpanded(index)
		if cv.navigationMode != NavigationModeMessageHistory {
			cv.updateViewportContentFull()
		}
	}
}

func (cv *ConversationView) ToggleAllToolResultsExpansion() {
	expand := !cv.anyToolResultExpanded()
	cv.allToolsExpanded = expand

	for i, entry := range cv.conversation {
		if entry.Message.Role == "tool" {
			cv.expandedToolResults[i] = expand
		}
	}

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContentFull()
	}
}

// anyToolResultExpanded reports whether any tool result is currently expanded,
// honoring per-tool defaults (e.g. Edit/MultiEdit diffs default to expanded).
func (cv *ConversationView) anyToolResultExpanded() bool {
	for i, entry := range cv.conversation {
		if entry.Message.Role == "tool" && cv.IsToolResultExpanded(i) {
			return true
		}
	}
	return false
}

// IsToolResultExpanded returns the effective expansion of a tool result: an
// explicit user choice (set via ctrl+o or a per-entry toggle) if present,
// otherwise the per-tool default from defaultExpandedTools.
func (cv *ConversationView) IsToolResultExpanded(index int) bool {
	if index < 0 || index >= len(cv.conversation) {
		return false
	}
	if v, ok := cv.expandedToolResults[index]; ok {
		return v
	}
	return cv.defaultExpanded(index)
}

// defaultExpanded reports whether the tool at index should render expanded
// before any explicit user choice - true for tools in defaultExpandedTools
// (Edit/MultiEdit), so their diffs are visible without a keypress.
func (cv *ConversationView) defaultExpanded(index int) bool {
	if index < 0 || index >= len(cv.conversation) {
		return false
	}
	te := cv.conversation[index].ToolExecution
	return te != nil && cv.defaultExpandedTools[te.ToolName]
}

// SetDefaultExpandedTools overrides which tool names render expanded by default.
func (cv *ConversationView) SetDefaultExpandedTools(names map[string]bool) {
	cv.defaultExpandedTools = names
}

func (cv *ConversationView) ToggleAllThinkingExpansion() {
	cv.allThinkingExpanded = !cv.allThinkingExpanded

	for i, entry := range cv.conversation {
		if entry.ReasoningContent != "" {
			cv.expandedThinkingBlocks[i] = cv.allThinkingExpanded
		}
	}

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContentFull()
	}
}

func (cv *ConversationView) IsThinkingExpanded(index int) bool {
	if expanded, exists := cv.expandedThinkingBlocks[index]; exists {
		return expanded
	}
	return false
}

// ToggleRawFormat toggles between raw and rendered markdown display
func (cv *ConversationView) ToggleRawFormat() {
	cv.rawFormat = !cv.rawFormat
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContentFull()
	}
}

// IsRawFormat returns true if raw format (no markdown rendering) is enabled
func (cv *ConversationView) IsRawFormat() bool {
	return cv.rawFormat
}

// RefreshTheme rebuilds the markdown renderer with current theme colors
func (cv *ConversationView) RefreshTheme() {
	if cv.markdownRenderer != nil {
		cv.markdownRenderer.RefreshTheme()
	}
	cv.renderCache = make(map[int]renderCacheEntry)
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContentFull()
	}
}

// GetPlainTextLines returns the conversation as plain text lines for selection mode
// This returns the actual rendered content that was displayed in the viewport,
// preserving the same text wrapping and formatting
func (cv *ConversationView) GetPlainTextLines() []string {
	lines := strings.Split(cv.renderedContent, "\n")

	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}

	return lines
}

// updatePlainTextLines updates the plain text representation of the conversation
func (cv *ConversationView) updatePlainTextLines() {
	if cv.lineFormatter != nil {
		cv.plainTextLines = cv.lineFormatter.FormatConversationToLines(cv.conversation)
	}
}

func (cv *ConversationView) SetWidth(width int) {
	cv.width = width
	cv.Viewport.SetWidth(width)
	if cv.lineFormatter != nil {
		cv.lineFormatter.SetWidth(width)
	}
	if cv.markdownRenderer != nil {
		cv.markdownRenderer.SetWidth(width)
	}
}

func (cv *ConversationView) SetHeight(height int) {
	cv.height = height
	cv.Viewport.SetHeight(height)
}

func (cv *ConversationView) Render() string {
	if cv.navigationMode == NavigationModeMessageHistory {
		cv.viewportMu.Lock()
		viewportContent := cv.Viewport.View()
		cv.viewportMu.Unlock()

		lines := strings.Split(viewportContent, "\n")
		leftPadding := "  "
		for i, line := range lines {
			lines[i] = leftPadding + strings.TrimRight(line, " ")
		}
		result := strings.Join(lines, "\n")
		return result
	}

	cv.viewportMu.Lock()
	if len(cv.conversation) == 0 {
		cv.Viewport.SetContent(cv.renderWelcome())
	}
	viewportContent := cv.Viewport.View()
	cv.viewportMu.Unlock()

	lines := strings.Split(viewportContent, "\n")

	leftPadding := "  "
	for i, line := range lines {
		lines[i] = leftPadding + strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func (cv *ConversationView) updateViewportContent() {
	cv.updateViewportContentFull()
}

// appendStreamingContent appends content to the streaming buffer and triggers immediate render
func (cv *ConversationView) appendStreamingContent(content, reasoning, model string) {
	cv.streamingMu.Lock()
	cv.isStreaming = true
	cv.streamingModel = model
	cv.streamingBuffer.WriteString(content)
	cv.streamingReasoningBuffer.WriteString(reasoning)
	cv.streamingMu.Unlock()

	cv.updateViewportContentFull()
}

// flushStreamingBuffer clears the streaming buffer after completion
func (cv *ConversationView) flushStreamingBuffer() {
	cv.streamingMu.Lock()
	defer cv.streamingMu.Unlock()

	cv.streamingBuffer.Reset()
	cv.streamingReasoningBuffer.Reset()
	cv.isStreaming = false
	cv.streamingModel = ""
}

// renderStreamingContent renders the currently streaming assistant message
func (cv *ConversationView) renderStreamingContent() string {
	cv.streamingMu.RLock()
	streamingContent := cv.streamingBuffer.String()
	streamingReasoning := cv.streamingReasoningBuffer.String()
	model := cv.streamingModel
	cv.streamingMu.RUnlock()

	var result strings.Builder

	if streamingReasoning != "" {
		isExpanded := cv.allThinkingExpanded || cv.expandedThinkingBlocks[-1]
		thinkingBlock := cv.renderThinkingBlock(streamingReasoning, -1, isExpanded)
		result.WriteString(thinkingBlock)
	}

	rolePrefixLength := 13
	if model != "" {
		rolePrefixLength += len(fmt.Sprintf(" (%s)", model))
	}

	wrapWidth := max(cv.width-rolePrefixLength, 40)

	streamingContent = formatting.FormatResponsiveMessage(streamingContent, wrapWidth)

	assistantColor := cv.styleProvider.GetThemeColor("assistant")
	var roleStyled string
	if model != "" {
		dimColor := cv.styleProvider.GetThemeColor("dim")
		rolePart := cv.styleProvider.RenderWithColor("⏺ Assistant", assistantColor)
		modelLabel := cv.styleProvider.RenderWithColor(fmt.Sprintf(" (%s)", model), dimColor)
		roleStyled = rolePart + modelLabel + cv.styleProvider.RenderWithColor(":", assistantColor)
	} else {
		roleStyled = cv.styleProvider.RenderWithColor("⏺ Assistant:", assistantColor)
	}

	result.WriteString(roleStyled)
	result.WriteString(" ")
	result.WriteString(streamingContent)
	result.WriteString("\n")
	return result.String()
}

// updateViewportContentFull performs a full rebuild of the viewport content
func (cv *ConversationView) updateViewportContentFull() {
	var b strings.Builder

	displayIndex := 0
	for i, entry := range cv.conversation {
		if entry.Hidden {
			continue
		}
		b.WriteString(cv.renderEntryCached(entry, i))
		b.WriteString("\n")
		displayIndex++
	}

	if cv.toolCallRenderer != nil {
		toolPreviews := cv.toolCallRenderer.RenderPreviews()
		if toolPreviews != "" {
			b.WriteString(toolPreviews)
			b.WriteString("\n\n")
		}
	}

	cv.streamingMu.RLock()
	shouldRenderStreaming := cv.isStreaming && (cv.streamingBuffer.Len() > 0 || cv.streamingReasoningBuffer.Len() > 0)
	cv.streamingMu.RUnlock()

	if shouldRenderStreaming {
		streamingText := cv.renderStreamingContent()
		b.WriteString(streamingText)
	}

	cv.renderedContent = b.String()

	cv.viewportMu.Lock()
	cv.Viewport.SetContent(cv.renderedContent)
	if !cv.userScrolledUp {
		cv.Viewport.GotoBottom()
	}
	cv.viewportMu.Unlock()
}

func (cv *ConversationView) renderWelcome() string {
	if cv.height >= 20 {
		return cv.renderFullWelcome()
	}
	return cv.renderCompactWelcome()
}

func (cv *ConversationView) renderFullWelcome() string {
	statusColor := cv.styleProvider.GetThemeColor("status")
	successColor := cv.styleProvider.GetThemeColor("success")
	dimColor := cv.styleProvider.GetThemeColor("dim")

	headerLine := cv.styleProvider.RenderWithColor("* Inference Gateway CLI", statusColor)
	readyLine := cv.styleProvider.RenderWithColor("> Ready to chat!", successColor)

	wd, err := os.Getwd()
	if err != nil {
		wd = "unknown"
	}

	headerColor := cv.getHeaderColor()
	workingLinePrefix := cv.styleProvider.RenderWithColor("@ Working in: ", dimColor)
	workingLinePath := cv.styleProvider.RenderWithColor(wd, headerColor)
	workingLine := workingLinePrefix + workingLinePath

	configLine := cv.buildConfigLine()
	versionLine := cv.buildVersionLine()

	var content string
	if versionLine != "" {
		content = headerLine + "\n\n" + readyLine + "\n\n" + workingLine + "\n\n" + configLine + "\n\n" + versionLine
	} else {
		content = headerLine + "\n\n" + readyLine + "\n\n" + workingLine + "\n\n" + configLine
	}

	return cv.styleProvider.RenderBorderedBox(content, cv.styleProvider.GetThemeColor("accent"), 1, 1)
}

func (cv *ConversationView) renderCompactWelcome() string {
	statusColor := cv.styleProvider.GetThemeColor("status")
	successColor := cv.styleProvider.GetThemeColor("success")
	dimColor := cv.styleProvider.GetThemeColor("dim")

	headerLine := cv.styleProvider.RenderWithColor("* Inference Gateway CLI", statusColor)
	readyLine := cv.styleProvider.RenderWithColor("> Ready to chat!", successColor)
	separator := cv.styleProvider.RenderWithColor("  •  ", dimColor)
	versionShort := cv.buildVersionShort()

	var content string
	if versionShort != "" {
		content = headerLine + separator + readyLine + separator + versionShort
	} else {
		content = headerLine + separator + readyLine
	}

	return cv.styleProvider.RenderBorderedBox(content, cv.styleProvider.GetThemeColor("accent"), 1, 1)
}

// renderCacheEntry is one memoized entry rendering.
type renderCacheEntry struct {
	fingerprint uint64
	rendered    string
}

// renderEntryCached returns the memoized rendering for the entry at index,
// re-rendering only when the fingerprint changes. Entries whose rendering
// reads live external state (pending plan approval buttons) bypass the cache.
func (cv *ConversationView) renderEntryCached(entry domain.ConversationEntry, index int) string {
	if entry.IsPlan && entry.PlanApprovalStatus == domain.PlanApprovalPending {
		return cv.renderEntryWithIndex(entry, index)
	}

	fp := cv.entryFingerprint(entry, index)
	if cached, ok := cv.renderCache[index]; ok && cached.fingerprint == fp {
		return cached.rendered
	}

	rendered := cv.renderEntryWithIndex(entry, index)
	cv.renderCache[index] = renderCacheEntry{fingerprint: fp, rendered: rendered}
	return rendered
}

// entryFingerprint hashes every input that affects an entry's rendered output.
// Message text is identified by the entry's creation time rather than hashed:
// entries are append-only, so post-creation changes only touch the mutable
// fields mixed in below (tool execution, approval statuses, expansion, width,
// raw mode).
func (cv *ConversationView) entryFingerprint(entry domain.ConversationEntry, index int) uint64 {
	h := fnv.New64a()
	var buf [8]byte

	writeInt := func(v int64) {
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		_, _ = h.Write(buf[:])
	}
	writeBool := func(b bool) {
		if b {
			writeInt(1)
		} else {
			writeInt(0)
		}
	}
	writeString := func(s string) {
		_, _ = h.Write([]byte(s))
		writeInt(int64(len(s)))
	}

	writeInt(entry.Time.UnixNano())
	writeString(string(entry.Message.Role))
	writeString(entry.Model)
	writeInt(int64(len(entry.ReasoningContent)))
	writeInt(int64(len(entry.Images)))
	writeBool(entry.Hidden)
	writeBool(entry.Rejected)
	writeBool(entry.IsPlan)
	writeInt(int64(entry.ToolApprovalStatus))
	writeInt(int64(entry.PlanApprovalStatus))
	writeBool(entry.PendingToolCall != nil)
	if entry.Message.ToolCalls != nil {
		writeInt(int64(len(*entry.Message.ToolCalls)))
	}
	if te := entry.ToolExecution; te != nil {
		writeString(te.ToolName)
		writeBool(te.Success)
		writeBool(te.Rejected)
		writeInt(int64(te.Duration))
		writeInt(int64(len(te.Error)))
		writeInt(int64(len(te.Diff)))
	}

	writeInt(int64(cv.width))
	writeBool(cv.rawFormat)
	writeBool(cv.IsToolResultExpanded(index))
	writeBool(cv.IsThinkingExpanded(index))
	return h.Sum64()
}

func (cv *ConversationView) renderEntryWithIndex(entry domain.ConversationEntry, index int) string {
	if handled, result := cv.tryRenderSpecialEntry(entry, index); handled {
		return result
	}

	color, role := cv.getRoleAndColor(entry)

	if entry.Hidden {
		return ""
	}

	return cv.renderStandardEntry(entry, index, color, role)
}

// tryRenderSpecialEntry attempts to render special entry types (user commands, plans, tools)
func (cv *ConversationView) tryRenderSpecialEntry(entry domain.ConversationEntry, index int) (bool, string) {
	switch string(entry.Message.Role) {
	case "user":
		if result := cv.tryRenderUserCommand(entry); result != "" {
			return true, result
		}
	case "assistant":
		if entry.IsPlan {
			return true, cv.renderPlanEntry(entry, index)
		}
		if entry.PendingToolCall != nil {
			return true, cv.renderPendingToolEntry(entry)
		}
		if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
			color, role := cv.getAssistantRoleAndColor(entry)
			return true, cv.renderAssistantWithToolCalls(entry, index, color, role)
		}
	case "tool":
		return true, cv.renderToolEntry(entry, index)
	}
	return false, ""
}

// tryRenderUserCommand checks if user entry is a command and renders it
func (cv *ConversationView) tryRenderUserCommand(entry domain.ConversationEntry) string {
	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		return ""
	}

	color := cv.getUserColor()
	role := "> You"

	if strings.HasPrefix(contentStr, "!!") {
		return cv.renderToolCommandEntry(entry, color, role, contentStr)
	}
	if strings.HasPrefix(contentStr, "!") {
		return cv.renderShellCommandEntry(entry, color, role, contentStr)
	}
	return ""
}

// getRoleAndColor returns the role label and color for a given entry
func (cv *ConversationView) getRoleAndColor(entry domain.ConversationEntry) (string, string) {
	switch string(entry.Message.Role) {
	case "user":
		return cv.getUserColor(), "> You"
	case "assistant":
		return cv.getAssistantRoleAndColor(entry)
	case "system":
		return cv.styleProvider.GetThemeColor("dim"), "⚙️ System"
	case "tool":
		return cv.getToolRoleAndColor(entry)
	default:
		return cv.styleProvider.GetThemeColor("dim"), string(entry.Message.Role)
	}
}

// getAssistantRoleAndColor returns role and color for assistant entries
func (cv *ConversationView) getAssistantRoleAndColor(entry domain.ConversationEntry) (string, string) {
	if entry.Rejected {
		return cv.styleProvider.GetThemeColor("dim"), "⊘ Rejected Plan"
	}
	return cv.getAssistantColor(), "⏺ Assistant"
}

// getToolRoleAndColor returns role and color for tool entries
func (cv *ConversationView) getToolRoleAndColor(entry domain.ConversationEntry) (string, string) {
	role := "🔧 Tool"
	if entry.ToolExecution != nil && !entry.ToolExecution.Success {
		return cv.styleProvider.GetThemeColor("error"), role
	}
	if entry.ToolExecution != nil && entry.ToolExecution.Success {
		return cv.styleProvider.GetThemeColor("success"), role
	}
	return cv.styleProvider.GetThemeColor("accent"), role
}

// renderStandardEntry renders a standard message entry
func (cv *ConversationView) renderStandardEntry(entry domain.ConversationEntry, index int, color, role string) string {
	var result strings.Builder

	if entry.Message.Role == sdk.Assistant && entry.ReasoningContent != "" {
		isExpanded := cv.IsThinkingExpanded(index)
		thinkingBlock := cv.renderThinkingBlock(entry.ReasoningContent, index, isExpanded)
		result.WriteString(thinkingBlock)
	}

	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}

	rolePrefixLength := len(role) + 2
	var modelLabelText string
	if entry.Message.Role == sdk.Assistant && entry.Model != "" && !entry.Rejected {
		modelLabelText = fmt.Sprintf(" (%s)", entry.Model)
		rolePrefixLength += len(modelLabelText)
	}

	wrapWidth := max(cv.width-rolePrefixLength, 40)

	roleStyled := cv.formatRoleWithModel(role, color, modelLabelText)

	if entry.Message.Role == sdk.Assistant && entry.Model == "" {
		cv.renderShortcutOutput(&result, roleStyled, contentStr, wrapWidth)
	} else {
		cv.renderInlineContent(&result, roleStyled, entry, contentStr, wrapWidth)
	}

	return result.String()
}

// formatRoleWithModel formats the role prefix with optional model label
func (cv *ConversationView) formatRoleWithModel(role, color, modelLabelText string) string {
	if modelLabelText == "" {
		return cv.styleProvider.RenderWithColor(role+":", color)
	}

	dimColor := cv.styleProvider.GetThemeColor("dim")
	rolePart := cv.styleProvider.RenderWithColor(role, color)
	modelLabel := cv.styleProvider.RenderWithColor(modelLabelText, dimColor)
	return rolePart + modelLabel + cv.styleProvider.RenderWithColor(":", color)
}

// renderShortcutOutput renders shortcut output on a new line with markdown support
func (cv *ConversationView) renderShortcutOutput(result *strings.Builder, roleStyled, contentStr string, wrapWidth int) {
	result.WriteString(roleStyled)
	result.WriteString("\n\n")
	formattedContent := cv.applyMarkdownIfEnabled(contentStr, wrapWidth)
	for line := range strings.SplitSeq(formattedContent, "\n") {
		result.WriteString("  ")
		result.WriteString(line)
		result.WriteString("\n")
	}
}

// renderInlineContent renders content inline with the role
func (cv *ConversationView) renderInlineContent(result *strings.Builder, roleStyled string, entry domain.ConversationEntry, contentStr string, wrapWidth int) {
	var formattedContent string
	if entry.Message.Role == sdk.Assistant && cv.markdownRenderer != nil && !cv.rawFormat {
		formattedContent = cv.applyMarkdownIfEnabled(contentStr, wrapWidth)
	} else {
		formattedContent = formatting.FormatResponsiveMessage(contentStr, wrapWidth)
	}
	result.WriteString(roleStyled)
	result.WriteString(" ")
	result.WriteString(formattedContent)
	result.WriteString("\n")
}

// applyMarkdownIfEnabled applies markdown rendering if enabled, otherwise formats as plain text
func (cv *ConversationView) applyMarkdownIfEnabled(contentStr string, wrapWidth int) string {
	if cv.markdownRenderer != nil && !cv.rawFormat {
		originalWidth := cv.width
		cv.markdownRenderer.SetWidth(wrapWidth)
		formattedContent := cv.markdownRenderer.Render(contentStr)
		cv.markdownRenderer.SetWidth(originalWidth)
		return formattedContent
	}
	return formatting.FormatResponsiveMessage(contentStr, wrapWidth)
}

func (cv *ConversationView) renderAssistantWithToolCalls(entry domain.ConversationEntry, index int, color, role string) string {
	var result strings.Builder

	if entry.ReasoningContent != "" {
		isExpanded := cv.IsThinkingExpanded(index)
		thinkingBlock := cv.renderThinkingBlock(entry.ReasoningContent, index, isExpanded)
		result.WriteString(thinkingBlock)
	}

	var roleStyled string
	if entry.Model != "" && !entry.Rejected {
		dimColor := cv.styleProvider.GetThemeColor("dim")
		rolePart := cv.styleProvider.RenderWithColor(role, color)
		modelLabel := cv.styleProvider.RenderWithColor(fmt.Sprintf(" (%s)", entry.Model), dimColor)
		roleStyled = rolePart + modelLabel + cv.styleProvider.RenderWithColor(":", color)
	} else {
		roleStyled = cv.styleProvider.RenderWithColor(role+":", color)
	}

	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = ""
	}

	if contentStr != "" {
		if entry.Model == "" {
			result.WriteString(roleStyled)
			result.WriteString("\n")
			for line := range strings.SplitSeq(contentStr, "\n") {
				result.WriteString("  ")
				result.WriteString(line)
				result.WriteString("\n")
			}
		} else {
			modelLabelLen := len(fmt.Sprintf(" (%s)", entry.Model))
			formattedContent := cv.formatAssistantContent(contentStr, role, modelLabelLen)
			result.WriteString(roleStyled)
			result.WriteString(" ")
			result.WriteString(formattedContent)
			result.WriteString("\n")
		}
	} else {
		result.WriteString(roleStyled)
		result.WriteString("\n")
	}

	return result.String()
}

// formatAssistantContent formats assistant message content with proper wrapping
func (cv *ConversationView) formatAssistantContent(contentStr, role string, modelLabelLen int) string {
	rolePrefixLength := len(role) + 2 + modelLabelLen
	wrapWidth := max(cv.width-rolePrefixLength, 40)

	if cv.markdownRenderer != nil && !cv.rawFormat {
		originalWidth := cv.width
		cv.markdownRenderer.SetWidth(wrapWidth)
		formattedContent := cv.markdownRenderer.Render(contentStr)
		cv.markdownRenderer.SetWidth(originalWidth)
		return formattedContent
	}

	return formatting.FormatResponsiveMessage(contentStr, wrapWidth)
}

func (cv *ConversationView) renderToolEntry(entry domain.ConversationEntry, index int) string {
	var isExpanded bool
	if index >= 0 {
		isExpanded = cv.IsToolResultExpanded(index)

		if entry.ToolExecution != nil && cv.toolFormatter != nil {
			if cv.toolFormatter.ShouldAlwaysExpandTool(entry.ToolExecution.ToolName) {
				isExpanded = true
			}
		}
	}

	content := cv.formatEntryContent(entry, isExpanded)

	return content + "\n"
}

func (cv *ConversationView) formatEntryContent(entry domain.ConversationEntry, isExpanded bool) string {
	if isExpanded {
		return cv.formatExpandedContent(entry)
	}
	return cv.formatCompactContent(entry)
}

func (cv *ConversationView) formatExpandedContent(entry domain.ConversationEntry) string {
	if entry.ToolExecution != nil && cv.toolFormatter != nil {
		return cv.toolFormatter.FormatToolResultExpanded(entry.ToolExecution, cv.width)
	}
	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}
	wrappedContent := formatting.FormatResponsiveMessage(contentStr, cv.width)
	hint := cv.getToggleToolHint("collapse all tool calls")
	return wrappedContent + "\n\n• " + hint
}

func (cv *ConversationView) formatCompactContent(entry domain.ConversationEntry) string {
	// Tool results own their themed status line, preview and expand hint.
	if entry.ToolExecution != nil && cv.toolFormatter != nil {
		return cv.toolFormatter.FormatToolResultForUI(entry.ToolExecution, cv.width)
	}
	hint := cv.getHintForEntry(entry)
	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}
	content := cv.formatToolContentCompact(contentStr)
	wrappedContent := formatting.FormatResponsiveMessage(content, cv.width)
	return wrappedContent + "\n• " + hint
}

func (cv *ConversationView) formatToolContentCompact(content string) string {
	if cv.toolFormatter == nil {
		lines := strings.Split(content, "\n")
		if len(lines) <= 4 {
			return content
		}
		return strings.Join(lines[:4], "\n") + "\n... (truncated)"
	}

	lines := strings.Split(content, "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if toolCall := cv.parseToolCallFromLine(trimmed); toolCall != nil {
			formattedCall := cv.toolFormatter.FormatToolCall(toolCall.Name, toolCall.Args)
			result = append(result, "Tool: "+formattedCall)
		} else {
			result = append(result, line)
		}
	}

	if len(result) <= 4 {
		return strings.Join(result, "\n")
	}
	return strings.Join(result[:4], "\n") + "\n... (truncated)"
}

type ToolCallInfo struct {
	Name string
	Args map[string]any
}

// parseToolCallFromLine parses a tool call from a line like "Tool: Write(content="...", file_path="...")"
func (cv *ConversationView) parseToolCallFromLine(line string) *ToolCallInfo {
	toolCallPattern := regexp.MustCompile(`^Tool:\s+([A-Za-z]+)\((.*)?\)$`)
	matches := toolCallPattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return nil
	}

	toolName := matches[1]
	argsString := matches[2]

	args := make(map[string]any)
	if argsString != "" {
		argPattern := regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)=("[^"]*"|[^,]+)`)
		argMatches := argPattern.FindAllStringSubmatch(argsString, -1)

		for _, argMatch := range argMatches {
			if len(argMatch) == 3 {
				key := argMatch[1]
				value := strings.Trim(argMatch[2], `"`)
				args[key] = value
			}
		}
	}

	return &ToolCallInfo{
		Name: toolName,
		Args: args,
	}
}

// renderThinkingBlock renders a thinking/reasoning block for assistant messages
func (cv *ConversationView) renderThinkingBlock(thinking string, _ int, expanded bool) string {
	if thinking == "" {
		return ""
	}

	if !expanded {
		preview := cv.extractThinkingPreview(thinking, 3)
		hint := cv.getToggleThinkingHint("expand")
		collapsedText := fmt.Sprintf("%s...\n• %s", preview, hint)
		return cv.styleProvider.RenderDimText(collapsedText) + "\n"
	}

	wrappedThinking := formatting.FormatResponsiveMessage(thinking, cv.width)
	hint := cv.getToggleThinkingHint("collapse")
	expandedText := fmt.Sprintf("%s\n• %s", wrappedThinking, hint)
	return cv.styleProvider.RenderDimText(expandedText) + "\n"
}

// extractThinkingPreview extracts the first N lines from thinking text for collapsed view
func (cv *ConversationView) extractThinkingPreview(text string, maxLines int) string {
	wrappedText := formatting.FormatResponsiveMessage(text, cv.width)
	lines := strings.Split(wrappedText, "\n")

	if len(lines) <= maxLines {
		return wrappedText
	}

	preview := strings.Join(lines[:maxLines], "\n")
	return preview
}

// getToggleThinkingHint returns the keybinding hint for toggling thinking blocks
func (cv *ConversationView) getToggleThinkingHint(action string) string {
	if cv.keyHintFormatter == nil {
		return ""
	}

	actionID := config.ActionID(config.NamespaceDisplay, "toggle_thinking")
	return cv.keyHintFormatter.GetKeyHint(actionID, action+" thinking")
}

// buildConfigLine constructs the configuration line for the welcome screen
func (cv *ConversationView) buildConfigLine() string {
	if cv.configPath == "" {
		return ""
	}

	configType := cv.getConfigType()
	displayPath := cv.shortenPath(cv.configPath)

	dimColor := cv.styleProvider.GetThemeColor("dim")
	accentColor := cv.styleProvider.GetThemeColor("accent")

	configPrefix := cv.styleProvider.RenderWithColor("⚙ Config: ", dimColor)
	pathStyled := cv.styleProvider.RenderWithColor(displayPath, accentColor)
	configTypeStyled := cv.styleProvider.RenderWithColor(" ("+configType+")", dimColor)

	return configPrefix + pathStyled + configTypeStyled
}

// buildVersionLine constructs the version line for the welcome screen (full layout)
func (cv *ConversationView) buildVersionLine() string {
	if cv.versionInfo == nil || cv.versionInfo.Version == "" {
		return ""
	}

	dimColor := cv.styleProvider.GetThemeColor("dim")
	accentColor := cv.styleProvider.GetThemeColor("accent")

	version := cv.versionInfo.Version
	if version == "dev" {
		version = "dev"
	}

	prefix := cv.styleProvider.RenderWithColor("• Version: ", dimColor)
	versionStyled := cv.styleProvider.RenderWithColor(version, accentColor)

	return prefix + versionStyled
}

// buildVersionShort constructs the short version for compact layout
func (cv *ConversationView) buildVersionShort() string {
	if cv.versionInfo == nil || cv.versionInfo.Version == "" {
		return ""
	}

	dimColor := cv.styleProvider.GetThemeColor("dim")
	version := cv.versionInfo.Version

	return cv.styleProvider.RenderWithColor(version, dimColor)
}

// getConfigType determines if the config is project-level or userspace
func (cv *ConversationView) getConfigType() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "project"
	}

	homePath := filepath.Join(homeDir, ".infer")
	if strings.Contains(cv.configPath, homePath) {
		return "userspace"
	}
	return "project"
}

// shortenPath shortens very long paths for display
func (cv *ConversationView) shortenPath(path string) string {
	if len(path) <= 50 {
		return path
	}

	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) <= 2 {
		return path
	}

	return "..." + string(filepath.Separator) + parts[len(parts)-2] + string(filepath.Separator) + parts[len(parts)-1]
}

// Bubble Tea interface
func (cv *ConversationView) Init() tea.Cmd { return nil }

func (cv *ConversationView) View() tea.View { return tea.NewView(cv.Render()) }

func (cv *ConversationView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if cmd = cv.handleMouseEvents(msg); cmd != nil {
		return cv, cmd
	}

	if cmd = cv.handleWindowSizeEvents(msg); cmd != nil {
		return cv, cmd
	}

	switch msg := msg.(type) {
	case domain.ApprovalSelectionChangedEvent:
		return cv.handleApprovalSelectionChanged(msg, cmd)
	case domain.PlanApprovalSelectionChangedEvent:
		return cv.handlePlanApprovalSelectionChanged(msg, cmd)
	case domain.UpdateHistoryEvent:
		return cv.handleUpdateHistoryEvent(msg, cmd)
	case domain.ToolCallPreviewEvent, domain.ToolCallUpdateEvent, domain.ToolCallReadyEvent,
		domain.ToolExecutionProgressEvent, domain.BashOutputChunkEvent, domain.ChatCompleteEvent:
		return cv.handleToolCallEvents(msg, cmd)
	case domain.BashCommandCompletedEvent:
		return cv.handleBashCommandCompletedEvent(msg, cmd)
	case domain.StreamingContentEvent:
		return cv.handleStreamingContentEvent(msg, cmd)
	case domain.ScrollRequestEvent:
		return cv.handleScrollRequestEvent(msg, cmd)
	case domain.A2ATaskSubmittedEvent:
		return cv.handleA2ATaskSubmitted(msg, cmd)
	case domain.A2ATaskStatusUpdateEvent:
		return cv.handleA2ATaskStatusUpdate(msg, cmd)
	case domain.A2ATaskCompletedEvent:
		return cv.handleA2ATaskCompleted(msg, cmd)
	case domain.A2ATaskFailedEvent:
		return cv.handleA2ATaskFailed(msg, cmd)
	case domain.SubagentSubmittedEvent:
		return cv.handleSubagentSubmitted(msg, cmd)
	case domain.SubagentCompletedEvent:
		return cv.handleSubagentTerminal(msg.SubagentID, msg.Label, "done", cmd)
	case domain.SubagentFailedEvent:
		return cv.handleSubagentTerminal(msg.SubagentID, msg.Label, "failed", cmd)
	case BackgroundTaskRemovalTickMsg:
		return cv.handleRemoveBackgroundTask(msg, cmd)
	case subagentRemovalTickMsg:
		return cv.handleRemoveSubagent(msg, cmd)
	case spinner.TickMsg:
		return cv.handleSpinnerTick(msg, cmd)
	default:
		return cv.handleDefaultEvents(msg, cmd)
	}
}

// handleMouseEvents processes mouse wheel events.
// Bubble Tea v2 split MouseMsg into concrete types - wheel-up events arrive
// as MouseWheelMsg with Button == MouseWheelUp.
func (cv *ConversationView) handleMouseEvents(msg tea.Msg) tea.Cmd {
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		if wheel.Button == tea.MouseWheelUp {
			cv.userScrolledUp = true
		}
	}
	return nil
}

// handleWindowSizeEvents processes window resize events
func (cv *ConversationView) handleWindowSizeEvents(msg tea.Msg) tea.Cmd {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		cv.SetWidth(windowMsg.Width)
		cv.height = windowMsg.Height
		if cv.navigationMode != NavigationModeMessageHistory {
			cv.updateViewportContentFull()
		} else {
			cv.updateMessageHistoryView()
		}
	}
	return nil
}

// handleApprovalSelectionChanged processes approval selection change events
func (cv *ConversationView) handleApprovalSelectionChanged(_ domain.ApprovalSelectionChangedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, cmd
}

// handlePlanApprovalSelectionChanged refreshes the conversation viewport so
// the highlighted plan-approval button reflects the new selection index.
func (cv *ConversationView) handlePlanApprovalSelectionChanged(_ domain.PlanApprovalSelectionChangedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, cmd
}

// handleUpdateHistoryEvent processes history update events
func (cv *ConversationView) handleUpdateHistoryEvent(msg domain.UpdateHistoryEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.flushStreamingBuffer()
		cv.SetConversation(msg.History)
	}
	return cv, cmd
}

// handleToolCallEvents processes tool call related events
func (cv *ConversationView) handleToolCallEvents(msg tea.Msg, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.toolCallRenderer != nil {
		cmd = cv.handleToolCallRendererEvents(msg, cmd)
	}
	return cv, cmd
}

// handleBashCommandCompletedEvent processes bash command completion events
func (cv *ConversationView) handleBashCommandCompletedEvent(msg domain.BashCommandCompletedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.SetConversation(msg.History)
		if cv.toolCallRenderer != nil {
			cv.toolCallRenderer.ClearPreviews()
		}
	}
	return cv, cmd
}

// handleStreamingContentEvent processes streaming content events
func (cv *ConversationView) handleStreamingContentEvent(msg domain.StreamingContentEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.appendStreamingContent(msg.Content, msg.ReasoningContent, msg.Model)
	}
	return cv, cmd
}

// handleScrollRequestEvent processes scroll request events
func (cv *ConversationView) handleScrollRequestEvent(msg domain.ScrollRequestEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.ComponentID == "conversation" {
		return cv.handleScrollRequest(msg)
	}
	return cv, cmd
}

// handleSpinnerTick processes spinner tick events. Tick messages carry a
// per-spinner ID, so the same tea.Msg may belong to either the
// ToolCallRenderer's spinner or our own backgroundSpinner - we forward
// it to both. Whichever one's ID matches advances its frame and returns
// the next-tick cmd; the other call is a no-op.
func (cv *ConversationView) handleSpinnerTick(msg spinner.TickMsg, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	var bgCmd tea.Cmd
	cv.backgroundSpinner, bgCmd = cv.backgroundSpinner.Update(msg)
	if bgCmd != nil && cv.hasActiveBackgroundTasks() {
		cv.backgroundSpinStep++
		cmd = tea.Batch(cmd, bgCmd)
	}

	if cv.toolCallRenderer != nil {
		updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
		cv.toolCallRenderer = updatedRenderer
		if cv.navigationMode != NavigationModeMessageHistory &&
			(cv.toolCallRenderer.HasActivePreviews() || cv.hasActiveBackgroundTasks()) {
			cv.updateViewportContent()
		}
		if rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	} else if cv.navigationMode != NavigationModeMessageHistory && cv.hasActiveBackgroundTasks() {
		cv.updateViewportContent()
	}
	return cv, cmd
}

// handleA2ATaskSubmitted records a newly submitted A2A task so its live
// progress can be rendered under the originating tool result.
func (cv *ConversationView) handleA2ATaskSubmitted(msg domain.A2ATaskSubmittedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.TaskID == "" {
		return cv, cmd
	}

	display, exists := cv.backgroundTasks[msg.TaskID]
	if !exists {
		display = &BackgroundTaskDisplay{TaskID: msg.TaskID}
		cv.backgroundTasks[msg.TaskID] = display
	}
	if msg.AgentName != "" {
		display.AgentName = msg.AgentName
	}
	if msg.AgentURL != "" && display.AgentURL == "" {
		display.AgentURL = msg.AgentURL
	}
	if display.State == "" {
		display.State = "submitted"
	}
	if display.StartedAt.IsZero() {
		if !msg.Timestamp.IsZero() {
			display.StartedAt = msg.Timestamp
		} else {
			display.StartedAt = time.Now()
		}
	}
	if display.Model == "" && cv.agentModelResolver != nil && display.AgentURL != "" {
		if m := cv.agentModelResolver(display.AgentURL); m != "" {
			display.Model = m
		}
	}

	startSpinner := !cv.hasOtherActiveBackgroundTasks(msg.TaskID) && !cv.hasActiveSubagents()

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}

	if startSpinner {
		cmd = tea.Batch(cmd, cv.backgroundSpinner.Tick)
	}
	return cv, cmd
}

// handleA2ATaskStatusUpdate refreshes the live state/message for an in-flight task.
func (cv *ConversationView) handleA2ATaskStatusUpdate(msg domain.A2ATaskStatusUpdateEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.TaskID == "" {
		return cv, cmd
	}

	display, exists := cv.backgroundTasks[msg.TaskID]
	if !exists {
		display = &BackgroundTaskDisplay{TaskID: msg.TaskID}
		cv.backgroundTasks[msg.TaskID] = display
	}
	if msg.AgentURL != "" && display.AgentURL == "" {
		display.AgentURL = msg.AgentURL
	}
	if msg.Status != "" {
		display.State = msg.Status
	}
	if msg.Message != "" {
		display.Message = msg.Message
	}
	if display.Model == "" && cv.agentModelResolver != nil && display.AgentURL != "" {
		if m := cv.agentModelResolver(display.AgentURL); m != "" {
			display.Model = m
		}
	}
	if display.StartedAt.IsZero() {
		if !msg.Timestamp.IsZero() {
			display.StartedAt = msg.Timestamp
		} else {
			display.StartedAt = time.Now()
		}
	}

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, cmd
}

// handleA2ATaskCompleted marks a task as successfully completed, captures
// the usage JSON from Task.metadata, and schedules auto-removal.
func (cv *ConversationView) handleA2ATaskCompleted(msg domain.A2ATaskCompletedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.TaskID == "" {
		return cv, cmd
	}

	display, exists := cv.backgroundTasks[msg.TaskID]
	if !exists {
		display = &BackgroundTaskDisplay{TaskID: msg.TaskID}
		cv.backgroundTasks[msg.TaskID] = display
	}
	if display.StartedAt.IsZero() {
		display.StartedAt = time.Now()
	}
	display.State = "completed"
	display.IsTerminal = true
	display.CompletedAt = time.Now()
	display.UsageJSON = extractA2AUsageJSON(msg.Result.Data)
	display.ExecutionStatsJSON = extractA2AExecutionStatsJSON(msg.Result.Data)
	if display.AgentName == "" {
		display.AgentName = extractA2AAgentName(msg.Result.Data)
	}

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, tea.Batch(cmd, scheduleBackgroundTaskRemoval(msg.TaskID))
}

// handleA2ATaskFailed marks a task as failed, captures the error, and
// schedules auto-removal.
func (cv *ConversationView) handleA2ATaskFailed(msg domain.A2ATaskFailedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.TaskID == "" {
		return cv, cmd
	}

	display, exists := cv.backgroundTasks[msg.TaskID]
	if !exists {
		display = &BackgroundTaskDisplay{TaskID: msg.TaskID}
		cv.backgroundTasks[msg.TaskID] = display
	}
	if display.StartedAt.IsZero() {
		display.StartedAt = time.Now()
	}
	display.State = "failed"
	display.IsTerminal = true
	display.CompletedAt = time.Now()
	display.ErrorMsg = msg.Error
	if display.AgentName == "" {
		display.AgentName = extractA2AAgentName(msg.Result.Data)
	}

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, tea.Batch(cmd, scheduleBackgroundTaskRemoval(msg.TaskID))
}

// handleRemoveBackgroundTask removes a terminal-state task indicator after
// its 5-second lingering window.
func (cv *ConversationView) handleRemoveBackgroundTask(msg BackgroundTaskRemovalTickMsg, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if _, exists := cv.backgroundTasks[msg.TaskID]; !exists {
		return cv, cmd
	}
	delete(cv.backgroundTasks, msg.TaskID)

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, cmd
}

// --- Subagent live tree (Agent tool) ---

// handleSubagentSubmitted records a newly dispatched subagent so its live
// progress renders in the sticky tree under the Agent tool call.
func (cv *ConversationView) handleSubagentSubmitted(msg domain.SubagentSubmittedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.SubagentID == "" {
		return cv, cmd
	}
	d, exists := cv.subagentTasks[msg.SubagentID]
	if !exists {
		d = &subagentDisplay{ID: msg.SubagentID, StartedAt: time.Now()}
		cv.subagentTasks[msg.SubagentID] = d
	}
	d.Label = subagentLabel(msg.Label, msg.SubagentID)
	d.Status = "running"
	d.IsTerminal = false

	startSpinner := !cv.hasActiveA2A() && !cv.hasActiveSubagentsExcept(msg.SubagentID)
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	if startSpinner {
		cmd = tea.Batch(cmd, cv.backgroundSpinner.Tick)
	}
	return cv, cmd
}

// handleSubagentTerminal marks a subagent done/failed and schedules its removal.
func (cv *ConversationView) handleSubagentTerminal(id, label, status string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if id == "" {
		return cv, cmd
	}
	d, exists := cv.subagentTasks[id]
	if !exists {
		d = &subagentDisplay{ID: id, StartedAt: time.Now()}
		cv.subagentTasks[id] = d
	}
	if label != "" || d.Label == "" {
		d.Label = subagentLabel(label, id)
	}
	d.Status = status
	d.IsTerminal = true
	d.CompletedAt = time.Now()

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, tea.Batch(cmd, scheduleSubagentRemoval(id))
}

// handleRemoveSubagent drops a terminal subagent's row from the tree.
func (cv *ConversationView) handleRemoveSubagent(msg subagentRemovalTickMsg, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if _, exists := cv.subagentTasks[msg.ID]; !exists {
		return cv, cmd
	}
	delete(cv.subagentTasks, msg.ID)
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, cmd
}

func scheduleSubagentRemoval(id string) tea.Cmd {
	return tea.Tick(backgroundTaskRemovalDelay, func(_ time.Time) tea.Msg {
		return subagentRemovalTickMsg{ID: id}
	})
}

func (cv *ConversationView) hasActiveA2A() bool {
	for _, d := range cv.backgroundTasks {
		if !d.IsTerminal {
			return true
		}
	}
	return false
}

func (cv *ConversationView) hasActiveSubagents() bool {
	for _, d := range cv.subagentTasks {
		if !d.IsTerminal {
			return true
		}
	}
	return false
}

func (cv *ConversationView) hasActiveSubagentsExcept(id string) bool {
	for sid, d := range cv.subagentTasks {
		if sid != id && !d.IsTerminal {
			return true
		}
	}
	return false
}

func subagentLabel(label, id string) string {
	if label != "" {
		return label
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func subagentElapsed(d *subagentDisplay) string {
	if d.StartedAt.IsZero() {
		return ""
	}
	end := time.Now()
	if d.IsTerminal && !d.CompletedAt.IsZero() {
		end = d.CompletedAt
	}
	secs := int(end.Sub(d.StartedAt).Seconds())
	if secs < 0 {
		secs = 0
	}
	return fmt.Sprintf("%ds", secs)
}

// renderSubagentTree renders the live subagent tree: a header line plus one
// indented child row per subagent, each showing the executing-tool spinner
// while running and a status icon when done.
func (cv *ConversationView) renderSubagentTree() string {
	if len(cv.subagentTasks) == 0 {
		return ""
	}
	ids := make([]string, 0, len(cv.subagentTasks))
	for id := range cv.subagentTasks {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	running := 0
	for _, d := range cv.subagentTasks {
		if !d.IsTerminal {
			running++
		}
	}

	header := cv.styleProvider.RenderWithColor("Agent", cv.styleProvider.GetThemeColor("accent")) +
		cv.styleProvider.RenderWithColor(fmt.Sprintf(" %d subagent(s), %d running", len(ids), running), cv.styleProvider.GetThemeColor("dim"))
	lines := []string{header}

	for i, id := range ids {
		d := cv.subagentTasks[id]
		branch := "├─ "
		if i == len(ids)-1 {
			branch = "└─ "
		}

		var icon, iconColor, bodyColor string
		switch d.Status {
		case "done":
			icon, iconColor, bodyColor = icons.CheckMark, "success", "dim"
		case "failed":
			icon, iconColor, bodyColor = icons.CrossMark, "error", "error"
		default:
			icon, iconColor, bodyColor = icons.GetSpinnerFrame(cv.backgroundSpinStep), "accent", "accent"
		}

		statusText := d.Status
		if !d.IsTerminal {
			statusText = "running " + subagentElapsed(d)
		}

		styledBranch := cv.styleProvider.RenderWithColor("  "+branch, cv.styleProvider.GetThemeColor("dim"))
		styledIcon := cv.styleProvider.RenderWithColor(icon, cv.styleProvider.GetThemeColor(iconColor))
		styledBody := cv.styleProvider.RenderWithColor(fmt.Sprintf("%s - %s", d.Label, statusText), cv.styleProvider.GetThemeColor(bodyColor))
		lines = append(lines, styledBranch+styledIcon+" "+styledBody)
	}
	return strings.Join(lines, "\n")
}

// hasActiveBackgroundTasks reports whether any tracked task (A2A or subagent)
// has not yet reached a terminal state - used to keep the spinner ticking only
// while needed.
func (cv *ConversationView) hasActiveBackgroundTasks() bool {
	return cv.hasActiveA2A() || cv.hasActiveSubagents()
}

// hasOtherActiveBackgroundTasks reports whether any non-terminal task other
// than `exceptTaskID` is being tracked.
func (cv *ConversationView) hasOtherActiveBackgroundTasks(exceptTaskID string) bool {
	for id, d := range cv.backgroundTasks {
		if id == exceptTaskID {
			continue
		}
		if !d.IsTerminal {
			return true
		}
	}
	return false
}

// scheduleBackgroundTaskRemoval returns a tea.Cmd that fires
// BackgroundTaskRemovalTickMsg after backgroundTaskRemovalDelay.
func scheduleBackgroundTaskRemoval(taskID string) tea.Cmd {
	return tea.Tick(backgroundTaskRemovalDelay, func(_ time.Time) tea.Msg {
		return BackgroundTaskRemovalTickMsg{TaskID: taskID}
	})
}

// HasBackgroundTasks reports whether there is at least one tracked
// background task (A2A task or local subagent) to render in the sticky bar.
func (cv *ConversationView) HasBackgroundTasks() bool {
	return len(cv.backgroundTasks) > 0 || len(cv.subagentTasks) > 0
}

// RenderBackgroundTasksBar returns the sticky multi-line indicator block
// rendered above the input area. Each tracked task gets one line. Order is
// stable (lexicographic by TaskID) so concurrent tasks don't jitter
// between renders. Returns "" when there are no tasks to show. The
// width controls non-terminal truncation of the model= segment; pass 0
// to disable truncation entirely (used in some tests).
func (cv *ConversationView) RenderBackgroundTasksBar(width int) string {
	lines := make([]string, 0)

	ids := make([]string, 0, len(cv.backgroundTasks))
	for id := range cv.backgroundTasks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if line := cv.renderBackgroundTaskLine(cv.backgroundTasks[id], width); line != "" {
			lines = append(lines, line)
		}
	}

	if tree := cv.renderSubagentTree(); tree != "" {
		lines = append(lines, tree)
	}

	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// BackgroundTasksBarHeight returns the line count the sticky indicator
// will occupy, for use in the parent layout's height budgeting. Terminal
// states render across multiple lines (header + usage + execution_stats),
// so this counts the actual rendered newlines rather than just the task
// count.
func (cv *ConversationView) BackgroundTasksBarHeight() int {
	if len(cv.backgroundTasks) == 0 {
		return 0
	}
	bar := cv.RenderBackgroundTasksBar(cv.width)
	if bar == "" {
		return 0
	}
	return strings.Count(bar, "\n") + 1
}

// normalizeTaskState turns ADK enum-style task states like
// "TASK_STATE_WORKING" into tidy user-facing strings like "working".
// Already-tidy inputs (e.g. "submitted") are returned unchanged.
func normalizeTaskState(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	lower = strings.TrimPrefix(lower, "task_state_")
	return strings.ReplaceAll(lower, "_", "-")
}

// agentDisplayName resolves the best available agent name for a display:
// configured friendly name first, then resolver lookup by URL, then the
// URL with scheme stripped (e.g. http://localhost:8081 → localhost:8081),
// then the task ID as a last-resort fallback.
func (cv *ConversationView) agentDisplayName(display *BackgroundTaskDisplay) string {
	if display.AgentName != "" {
		return display.AgentName
	}
	if cv.agentNameResolver != nil && display.AgentURL != "" {
		if resolved := cv.agentNameResolver(display.AgentURL); resolved != "" {
			return resolved
		}
	}
	if display.AgentURL != "" {
		return shortenAgentURL(display.AgentURL)
	}
	return display.TaskID
}

// formatTerminalHeader builds the single-line header for a terminal-state
// background-task indicator. Appends the frozen elapsed suffix when
// available. The model= segment is intentionally omitted in terminal
// form - by completion, focus shifts to usage= / execution_stats=.
func (cv *ConversationView) formatTerminalHeader(name, state string, display *BackgroundTaskDisplay) string {
	elapsed := computeElapsedString(display)
	if elapsed == "" {
		return fmt.Sprintf("Agent(%s=%s)", name, state)
	}
	return fmt.Sprintf("Agent(%s=%s) %s", name, state, elapsed)
}

// formatNonTerminalBody assembles the inline body for a non-terminal
// background-task indicator. Shape:
//
//	Agent(<name>=<state>..., model=<model>) <elapsed>
//
// The model segment is dropped cleanly when unknown:
//
//	Agent(<name>=<state>...) <elapsed>
//
// When width > 0 and the assembled body would exceed the available
// columns, only the model value is truncated with a trailing "...";
// name, state, and elapsed are preserved verbatim. If even the framing
// can't fit, the model segment is dropped rather than producing
// garbled output.
func (cv *ConversationView) formatNonTerminalBody(name, state string, display *BackgroundTaskDisplay, width int) string {
	elapsed := computeElapsedString(display)
	model := strings.TrimSpace(display.Model)

	var body string
	switch {
	case model != "" && elapsed != "":
		body = fmt.Sprintf("Agent(%s=%s..., model=%s) %s", name, state, model, elapsed)
	case model != "":
		body = fmt.Sprintf("Agent(%s=%s..., model=%s)", name, state, model)
	case elapsed != "":
		body = fmt.Sprintf("Agent(%s=%s...) %s", name, state, elapsed)
	default:
		body = fmt.Sprintf("Agent(%s=%s...)", name, state)
	}

	if width <= 0 || model == "" {
		return body
	}
	const iconBudget = 2
	avail := width - iconBudget
	if len(body) <= avail {
		return body
	}

	var framing string
	if elapsed != "" {
		framing = fmt.Sprintf("Agent(%s=%s..., model=) %s", name, state, elapsed)
	} else {
		framing = fmt.Sprintf("Agent(%s=%s..., model=)", name, state)
	}
	const ellipsis = "..."
	modelBudget := avail - len(framing) - len(ellipsis)
	if modelBudget <= 0 {
		if elapsed != "" {
			return fmt.Sprintf("Agent(%s=%s...) %s", name, state, elapsed)
		}
		return fmt.Sprintf("Agent(%s=%s...)", name, state)
	}
	truncated := model[:modelBudget] + ellipsis
	if elapsed != "" {
		return fmt.Sprintf("Agent(%s=%s..., model=%s) %s", name, state, truncated, elapsed)
	}
	return fmt.Sprintf("Agent(%s=%s..., model=%s)", name, state, truncated)
}

// renderTerminalMultiLine emits a multi-line indicator for terminal
// states: header (icon + Agent(name=state)), then `usage=…`,
// `execution_stats=…`, and (for failures) `error: …`, each on its own
// line under a tree branch (├── / └──) - matching the box-drawing
// hierarchy style used elsewhere in the UI. Detail lines that have no
// content are dropped, so older agents without usage metadata render
// just the header.
func (cv *ConversationView) renderTerminalMultiLine(icon, iconColor, stateColor, header, usageJSON, statsJSON, err string) string {
	headerStyled := cv.styleProvider.RenderWithColor(icon, cv.styleProvider.GetThemeColor(iconColor)) +
		" " + cv.styleProvider.RenderWithColor(header, cv.styleProvider.GetThemeColor(stateColor))

	dim := cv.styleProvider.GetThemeColor("dim")
	errColor := cv.styleProvider.GetThemeColor("error")

	type branch struct {
		text  string
		color string
	}
	var details []branch
	if usageJSON != "" {
		details = append(details, branch{"usage=" + usageJSON, dim})
	}
	if statsJSON != "" {
		details = append(details, branch{"execution_stats=" + statsJSON, dim})
	}
	if err != "" {
		details = append(details, branch{"error: " + err, errColor})
	}

	if len(details) == 0 {
		return headerStyled
	}

	lines := []string{headerStyled}
	for i, d := range details {
		prefix := "├── "
		if i == len(details)-1 {
			prefix = "└── "
		}
		lines = append(lines, "  "+cv.styleProvider.RenderWithColor(prefix+d.text, d.color))
	}
	return strings.Join(lines, "\n")
}

// shortenAgentURL produces a tidier label for an agent URL when no
// configured friendly name is available. Strips the scheme and any
// trailing path/query so a raw URL like "http://localhost:8081/api"
// renders as "localhost:8081".
func shortenAgentURL(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	if idx := strings.IndexAny(url, "/?#"); idx >= 0 {
		url = url[:idx]
	}
	return url
}

// renderBackgroundTaskLine produces one styled line summarising a background
// task's current state for inline display under the originating tool call.
// The width controls non-terminal truncation of the "model=" segment;
// pass 0 to disable truncation.
func (cv *ConversationView) renderBackgroundTaskLine(display *BackgroundTaskDisplay, width int) string {
	if display == nil {
		return ""
	}

	name := cv.agentDisplayName(display)
	state := normalizeTaskState(display.State)
	if state == "" {
		state = "submitted"
	}

	var (
		icon       string
		iconColor  string
		stateColor string
		body       string
	)

	switch state {
	case "completed":
		return cv.renderTerminalMultiLine(
			icons.CheckMark, "success", "dim",
			cv.formatTerminalHeader(name, "completed", display),
			display.UsageJSON, display.ExecutionStatsJSON, "",
		)
	case "failed":
		return cv.renderTerminalMultiLine(
			icons.CrossMark, "error", "error",
			cv.formatTerminalHeader(name, "failed", display),
			display.UsageJSON, display.ExecutionStatsJSON, display.ErrorMsg,
		)
	case "cancelled", "canceled":
		icon = icons.CrossMark
		iconColor = "dim"
		stateColor = "dim"
		body = cv.formatTerminalHeader(name, "cancelled", display)
	default:
		icon = icons.GetSpinnerFrame(cv.backgroundSpinStep)
		iconColor = "accent"
		stateColor = "accent"
		body = cv.formatNonTerminalBody(name, state, display, width)
	}

	styledIcon := cv.styleProvider.RenderWithColor(icon, cv.styleProvider.GetThemeColor(iconColor))
	styledBody := cv.styleProvider.RenderWithColor(body, cv.styleProvider.GetThemeColor(stateColor))
	return fmt.Sprintf("%s %s", styledIcon, styledBody)
}

// extractA2AAgentName attempts to derive a short agent identifier from an
// A2A_SubmitTask tool result. Falls back to the raw agent_url if no
// friendlier name is encoded.
func extractA2AAgentName(data any) string {
	if data == nil {
		return ""
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	var probe struct {
		AgentURL string `json:"agent_url"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.AgentURL
}

// extractA2AUsageJSON extracts a compact JSON string for Task.metadata.usage
// from an A2A_SubmitTask tool result. Returns "" if the metadata is absent
// (e.g. remote agent has EnableUsageMetadata disabled, or the task ran no
// LLM calls).
func extractA2AUsageJSON(data any) string {
	return extractTaskMetadataField(data, "usage")
}

// extractA2AExecutionStatsJSON extracts a compact JSON string for
// Task.metadata.execution_stats from an A2A_SubmitTask tool result. Per
// ADK ≥ 0.19.0 this is always present on terminal-state tasks when
// EnableUsageMetadata is on, even if no LLM calls were made (so tool-only
// agents still report tool_calls / failed_tools).
func extractA2AExecutionStatsJSON(data any) string {
	return extractTaskMetadataField(data, "execution_stats")
}

// extractTaskMetadataField pulls one top-level key out of
// Task.Metadata via a json round-trip on the A2A_SubmitTask result Data.
// JSON round-trip avoids importing internal/agent/tools (cyclic).
func extractTaskMetadataField(data any, field string) string {
	if data == nil {
		return ""
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	var probe struct {
		Task *struct {
			Metadata *map[string]any `json:"metadata"`
		} `json:"task"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	if probe.Task == nil || probe.Task.Metadata == nil {
		return ""
	}
	val, ok := (*probe.Task.Metadata)[field]
	if !ok || val == nil {
		return ""
	}
	out, err := json.Marshal(val)
	if err != nil {
		return ""
	}
	return string(out)
}

// formatElapsed renders a duration as "17s" for <60s, "1m23s" for >=60s.
// Negative durations render as "0s".
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	return fmt.Sprintf("%dm%ds", total/60, total%60)
}

// computeElapsedString returns the trailing "<elapsed>" string for a
// background-task indicator. Terminal states are frozen at
// CompletedAt-StartedAt; non-terminal states use time.Since(StartedAt).
// Returns "" if StartedAt is zero.
func computeElapsedString(display *BackgroundTaskDisplay) string {
	if display == nil || display.StartedAt.IsZero() {
		return ""
	}
	if display.IsTerminal && !display.CompletedAt.IsZero() {
		return formatElapsed(display.CompletedAt.Sub(display.StartedAt))
	}
	return formatElapsed(time.Since(display.StartedAt))
}

// handleDefaultEvents processes all other events
func (cv *ConversationView) handleDefaultEvents(msg tea.Msg, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if _, isKeyMsg := msg.(tea.KeyMsg); !isKeyMsg {
		cv.Viewport, cmd = cv.Viewport.Update(msg)
		if cv.Viewport.AtBottom() {
			cv.userScrolledUp = false
		}
	}
	return cv, cmd
}

func (cv *ConversationView) handleScrollRequest(msg domain.ScrollRequestEvent) (tea.Model, tea.Cmd) {
	switch msg.Direction {
	case domain.ScrollUp:
		cv.userScrolledUp = true
		for i := 0; i < msg.Amount; i++ {
			cv.Viewport.ScrollUp(1)
		}
	case domain.ScrollDown:
		for i := 0; i < msg.Amount; i++ {
			cv.Viewport.ScrollDown(1)
		}
		if cv.Viewport.AtBottom() {
			cv.userScrolledUp = false
		}
	case domain.ScrollToTop:
		cv.userScrolledUp = true
		cv.Viewport.GotoTop()
	case domain.ScrollToBottom:
		cv.userScrolledUp = false
		cv.Viewport.GotoBottom()
	}
	return cv, nil
}

// Helper methods to get theme colors with fallbacks
func (cv *ConversationView) getUserColor() string {
	return cv.styleProvider.GetThemeColor("user")
}

func (cv *ConversationView) getAssistantColor() string {
	return cv.styleProvider.GetThemeColor("assistant")
}

func (cv *ConversationView) getHeaderColor() string {
	return cv.styleProvider.GetThemeColor("accent")
}

// renderShellCommandEntry renders a shell command entry with highlighted prefix and proper spacing
func (cv *ConversationView) renderShellCommandEntry(_ domain.ConversationEntry, color, role, contentStr string) string {
	roleStyled := cv.styleProvider.RenderWithColor(role+":", color)

	command := strings.TrimPrefix(contentStr, "!")

	accentColor := cv.styleProvider.GetThemeColor("accent")
	prefixStyled := cv.styleProvider.RenderWithColor("!", accentColor)

	formattedContent := prefixStyled + " " + command
	wrappedContent := formatting.FormatResponsiveMessage(formattedContent, cv.width)

	message := roleStyled + " " + wrappedContent
	return message + "\n"
}

// renderToolCommandEntry renders a tool command entry (!! prefix) with highlighted prefix
func (cv *ConversationView) renderToolCommandEntry(_ domain.ConversationEntry, color, role, contentStr string) string {
	roleStyled := cv.styleProvider.RenderWithColor(role+":", color)

	command := strings.TrimPrefix(contentStr, "!!")

	accentColor := cv.styleProvider.GetThemeColor("accent")
	prefixStyled := cv.styleProvider.RenderWithColor("!!", accentColor)

	formattedContent := prefixStyled + " " + command
	wrappedContent := formatting.FormatResponsiveMessage(formattedContent, cv.width)

	message := roleStyled + " " + wrappedContent
	return message + "\n"
}

// renderPlanEntry renders the plan body as a regular markdown-rendered
// assistant message under a status-aware header, followed by inline
// approval buttons while approval is pending.
func (cv *ConversationView) renderPlanEntry(entry domain.ConversationEntry, index int) string {
	var result strings.Builder

	color, role := cv.planRoleAndColor(entry)
	roleStyled := cv.styleProvider.RenderWithColor(role+":", color)

	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}

	wrapWidth := max(cv.width-2, 40)

	var formattedContent string
	switch entry.PlanApprovalStatus {
	case domain.PlanApprovalRejected:
		plain := formatting.FormatResponsiveMessage(contentStr, wrapWidth)
		formattedContent = cv.styleProvider.RenderWithColor(plain, color)
	default:
		formattedContent = cv.applyMarkdownIfEnabled(contentStr, wrapWidth)
	}

	result.WriteString(roleStyled)
	result.WriteString("\n\n")
	for line := range strings.SplitSeq(formattedContent, "\n") {
		if line == "" {
			result.WriteString("\n")
			continue
		}
		result.WriteString("  ")
		result.WriteString(line)
		result.WriteString("\n")
	}

	if entry.PlanApprovalStatus == domain.PlanApprovalPending {
		result.WriteString("\n")
		result.WriteString(cv.renderInlineApprovalButtons(index))
		result.WriteString("\n")
	}

	return result.String() + "\n"
}

// planRoleAndColor returns the role label + theme color for a plan entry
// based on its approval status.
func (cv *ConversationView) planRoleAndColor(entry domain.ConversationEntry) (string, string) {
	switch entry.PlanApprovalStatus {
	case domain.PlanApprovalPending:
		return cv.styleProvider.GetThemeColor("accent"), "Plan (Pending Approval)"
	case domain.PlanApprovalAccepted:
		return cv.styleProvider.GetThemeColor("success"), "Plan (Accepted)"
	case domain.PlanApprovalRejected:
		return cv.styleProvider.GetThemeColor("dim"), "Plan (Rejected)"
	default:
		return cv.getAssistantColor(), "Plan"
	}
}

// renderInlineApprovalButtons renders inline approval buttons for a plan
func (cv *ConversationView) renderInlineApprovalButtons(_ int) string {
	selectedIndex := 0
	if cv.stateManager != nil {
		if planState := cv.stateManager.GetPlanApprovalUIState(); planState != nil {
			selectedIndex = planState.SelectedIndex
		}
	}

	acceptText := "Accept"
	rejectText := "Reject"
	standardText := "Approve Each Step"

	successColor := cv.styleProvider.GetThemeColor("success")
	errorColor := cv.styleProvider.GetThemeColor("error")
	accentColor := cv.styleProvider.GetThemeColor("accent")
	highlightBg := cv.styleProvider.GetThemeColor("selection_bg")

	var acceptStyled, rejectStyled, standardStyled string
	if selectedIndex == int(domain.PlanApprovalAccept) {
		acceptStyled = cv.styleProvider.RenderStyledText("[ "+acceptText+" ]", styles.StyleOptions{
			Foreground: successColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		acceptStyled = cv.styleProvider.RenderWithColor("[ "+acceptText+" ]", successColor)
	}

	if selectedIndex == int(domain.PlanApprovalReject) {
		rejectStyled = cv.styleProvider.RenderStyledText("[ "+rejectText+" ]", styles.StyleOptions{
			Foreground: errorColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		rejectStyled = cv.styleProvider.RenderWithColor("[ "+rejectText+" ]", errorColor)
	}

	if selectedIndex == int(domain.PlanApprovalAcceptStandard) {
		standardStyled = cv.styleProvider.RenderStyledText("[ "+standardText+" ]", styles.StyleOptions{
			Foreground: accentColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		standardStyled = cv.styleProvider.RenderWithColor("[ "+standardText+" ]", accentColor)
	}

	return fmt.Sprintf("  %s  %s  %s", acceptStyled, rejectStyled, standardStyled)
}

// renderPendingToolEntry renders a pending tool call that requires approval
// renderEditToolArgs renders the Edit tool arguments with a diff
func (cv *ConversationView) renderEditToolArgs(args map[string]any) string {
	var result strings.Builder

	oldStr, hasOld := args["old_string"].(string)
	newStr, hasNew := args["new_string"].(string)
	filePath, hasPath := args["file_path"].(string)

	if hasOld && hasNew && hasPath {
		fmt.Fprintf(&result, "  File: %s\n\n", filePath)
		diffRenderer := NewDiffRenderer(cv.styleProvider).SetContextLines(InlineDiffContextLines)
		diffInfo := DiffInfo{
			FilePath:   filePath,
			OldContent: oldStr,
			NewContent: newStr,
			Title:      "← Proposed Changes →",
		}
		diff := diffRenderer.RenderDiff(diffInfo)
		result.WriteString(diff)
		result.WriteString("\n")
	}

	return result.String()
}

// renderWriteToolArgs renders the Write tool arguments with content preview
func (cv *ConversationView) renderWriteToolArgs(args map[string]any) string {
	var result strings.Builder

	if filePath, ok := args["file_path"].(string); ok {
		fmt.Fprintf(&result, "  File: %s\n", filePath)
	}
	if content, ok := args["content"].(string); ok {
		preview := content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		fmt.Fprintf(&result, "  Content: %s\n", preview)
	}

	return result.String()
}

// renderRequestPlanApprovalArgs renders RequestPlanApproval arguments with the plan content
func (cv *ConversationView) renderRequestPlanApprovalArgs(args map[string]any) string {
	var result strings.Builder

	if plan, ok := args["plan"].(string); ok && plan != "" {
		result.WriteString("  Plan:\n\n")
		cv.renderIndentedPlanContent(&result, plan)
	}

	return result.String()
}

// renderIndentedPlanContent renders plan content with proper indentation
func (cv *ConversationView) renderIndentedPlanContent(result *strings.Builder, content string) {
	var rendered string
	if cv.markdownRenderer != nil && !cv.rawFormat {
		rendered = cv.markdownRenderer.Render(content)
	} else {
		rendered = formatting.FormatResponsiveMessage(content, cv.width)
	}

	for line := range strings.SplitSeq(rendered, "\n") {
		if line != "" {
			result.WriteString("    ")
			result.WriteString(line)
			result.WriteString("\n")
		} else {
			result.WriteString("\n")
		}
	}
}

func (cv *ConversationView) renderPendingToolEntry(entry domain.ConversationEntry) string {
	if entry.ToolApprovalStatus == domain.ToolApprovalPending {
		return ""
	}

	toolName := entry.PendingToolCall.Function.Name

	var args map[string]any
	_ = json.Unmarshal([]byte(entry.PendingToolCall.Function.Arguments), &args)

	var result strings.Builder
	result.WriteString(cv.renderApprovalHeader(toolName, args, entry.ToolApprovalStatus))
	result.WriteString("\n")

	switch toolName {
	case "Edit":
		result.WriteString(cv.renderEditToolArgs(args))
	case "Write":
		result.WriteString(cv.renderWriteToolArgs(args))
	case "RequestPlanApproval":
		result.WriteString(cv.renderRequestPlanApprovalArgs(args))
	}

	return result.String() + "\n"
}

// renderApprovalHeader renders a themed one-line header for an approved/rejected tool
// call, mirroring the completed result status line: "<icon> Name(args) · <status>".
func (cv *ConversationView) renderApprovalHeader(toolName string, args map[string]any, status domain.ToolApprovalStatus) string {
	icon := icons.CheckMark
	colorName := "success"
	label := "Approved"
	if status == domain.ToolApprovalRejected {
		icon = icons.CrossMark
		colorName = "error"
		label = "Rejected"
	}

	color := cv.styleProvider.GetThemeColor(colorName)
	styledIcon := cv.styleProvider.RenderWithColor(icon, color)
	styledLabel := cv.styleProvider.RenderWithColor("· "+label, color)

	call := toolName + "()"
	if cv.toolFormatter != nil && len(args) > 0 {
		call = cv.toolFormatter.FormatToolCall(toolName, args)
	}

	return fmt.Sprintf("%s %s %s", styledIcon, call, styledLabel)
}

// handleToolCallRendererEvents processes tool call renderer specific events
func (cv *ConversationView) handleToolCallRendererEvents(msg tea.Msg, cmd tea.Cmd) tea.Cmd {
	switch msg := msg.(type) {
	case domain.ToolCallPreviewEvent:
		updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
		cv.toolCallRenderer = updatedRenderer
		if rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.ToolCallUpdateEvent:
		updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
		cv.toolCallRenderer = updatedRenderer
		if rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.ToolCallReadyEvent:
		updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
		cv.toolCallRenderer = updatedRenderer
		if rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.ToolExecutionProgressEvent:
		updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
		cv.toolCallRenderer = updatedRenderer
		if rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.BashOutputChunkEvent:
		updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
		cv.toolCallRenderer = updatedRenderer
		if rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.ChatCompleteEvent:
		updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
		cv.toolCallRenderer = updatedRenderer
		if rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	}

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cmd
}

// getHintForEntry returns the appropriate hint based on entry state
func (cv *ConversationView) getHintForEntry(_ domain.ConversationEntry) string {
	return cv.getToggleToolHint("expand all tool calls")
}

func (cv *ConversationView) getToggleToolHint(action string) string {
	if cv.keyHintFormatter == nil {
		return ""
	}

	actionID := config.ActionID(config.NamespaceTools, "toggle_tool_expansion")
	return cv.keyHintFormatter.GetKeyHint(actionID, action)
}

// Message History Navigation Methods

// EnterMessageHistoryMode switches the conversation view to message history navigation mode
func (cv *ConversationView) EnterMessageHistoryMode(snapshots []domain.MessageSnapshot) {
	cv.navigationMode = NavigationModeMessageHistory
	cv.messageSnapshots = snapshots
	if len(snapshots) > 0 {
		cv.historySelectedIndex = len(snapshots) - 1
	} else {
		cv.historySelectedIndex = 0
	}
	cv.updateMessageHistoryView()
	cv.Viewport.GotoTop()
}

// ExitMessageHistoryMode returns the conversation view to normal mode
func (cv *ConversationView) ExitMessageHistoryMode() {
	cv.navigationMode = NavigationModeNormal
	cv.messageSnapshots = nil
	cv.historySelectedIndex = 0
	cv.updateViewportContentFull()
}

// IsInMessageHistoryMode returns true if currently in message history navigation mode
func (cv *ConversationView) IsInMessageHistoryMode() bool {
	return cv.navigationMode == NavigationModeMessageHistory
}

// NavigateHistoryUp moves the selection up in message history
func (cv *ConversationView) NavigateHistoryUp() {
	if len(cv.messageSnapshots) == 0 {
		return
	}
	if cv.historySelectedIndex > 0 {
		cv.historySelectedIndex--
		cv.updateMessageHistoryView()
	}
}

// NavigateHistoryDown moves the selection down in message history
func (cv *ConversationView) NavigateHistoryDown() {
	if len(cv.messageSnapshots) == 0 {
		return
	}
	if cv.historySelectedIndex < len(cv.messageSnapshots)-1 {
		cv.historySelectedIndex++
		cv.updateMessageHistoryView()
	}
}

// GetSelectedMessageIndex returns the conversation index of the selected message
func (cv *ConversationView) GetSelectedMessageIndex() int {
	if len(cv.messageSnapshots) == 0 || cv.historySelectedIndex < 0 || cv.historySelectedIndex >= len(cv.messageSnapshots) {
		return -1
	}
	return cv.messageSnapshots[cv.historySelectedIndex].Index
}

// GetSelectedMessageSnapshot returns the full snapshot of the selected message
func (cv *ConversationView) GetSelectedMessageSnapshot() *domain.MessageSnapshot {
	if len(cv.messageSnapshots) == 0 || cv.historySelectedIndex < 0 ||
		cv.historySelectedIndex >= len(cv.messageSnapshots) {
		return nil
	}
	snapshot := cv.messageSnapshots[cv.historySelectedIndex]
	return &snapshot
}

// updateMessageHistoryView updates the viewport content with the message history selector
func (cv *ConversationView) updateMessageHistoryView() {
	content := cv.renderMessageHistorySelector()
	cv.Viewport.SetContent(content)
}

// renderMessageHistorySelector renders the message history selector interface
func (cv *ConversationView) renderMessageHistorySelector() string {
	var b strings.Builder

	header := "# Message History\n\n"
	header += "_Select a restore point to rewind your conversation_\n\n"

	if cv.styleProvider != nil && cv.markdownRenderer != nil && !cv.rawFormat {
		cv.markdownRenderer.SetWidth(cv.width)
		b.WriteString(cv.markdownRenderer.Render(header))
	} else {
		title := "Message History"
		subtitle := "Select a restore point to rewind your conversation"
		if cv.styleProvider != nil {
			b.WriteString(cv.styleProvider.RenderWithColor(title, "accent"))
			b.WriteString("\n")
			b.WriteString(cv.styleProvider.RenderDimText(subtitle))
		} else {
			b.WriteString(title)
			b.WriteString("\n")
			b.WriteString(subtitle)
		}
		b.WriteString("\n\n")
	}

	countText := fmt.Sprintf("**%d messages** available for restoration", len(cv.messageSnapshots))
	if cv.styleProvider != nil && cv.markdownRenderer != nil && !cv.rawFormat {
		b.WriteString(cv.markdownRenderer.Render(countText))
	} else {
		plainCount := fmt.Sprintf("%d messages available for restoration", len(cv.messageSnapshots))
		if cv.styleProvider != nil {
			b.WriteString(cv.styleProvider.RenderDimText(plainCount))
		} else {
			b.WriteString(plainCount)
		}
	}
	b.WriteString("\n\n")

	if len(cv.messageSnapshots) == 0 {
		emptyText := "No messages to restore"
		if cv.styleProvider != nil {
			b.WriteString(cv.styleProvider.RenderDimText(emptyText))
		} else {
			b.WriteString(emptyText)
		}
		b.WriteString("\n\n")
		return b.String()
	}

	maxVisible := max(cv.height-10, 5)

	start, end := cv.calculatePaginationBounds(maxVisible)

	b.WriteString("\n")

	for i := start; i < end; i++ {
		msg := cv.messageSnapshots[i]
		isSelected := i == cv.historySelectedIndex

		timestamp := msg.Timestamp.Format("15:04:05")
		roleIndicator := "User"
		if msg.Role == sdk.Assistant {
			roleIndicator = "Assistant"
		}

		prefixWidth := 25
		availableWidth := max(cv.width-prefixWidth, 20)

		truncatedMsg := strings.ReplaceAll(msg.Content, "\n", " ")
		truncatedMsg = strings.ReplaceAll(truncatedMsg, "\r", " ")
		truncatedMsg = strings.Join(strings.Fields(truncatedMsg), " ")

		truncatedMsg = formatting.TruncateText(truncatedMsg, availableWidth)

		var entry string
		if isSelected {
			entry = fmt.Sprintf("▶ [%s] [%s] %s", timestamp, roleIndicator, truncatedMsg)
			if cv.styleProvider != nil {
				entry = cv.styleProvider.RenderWithColor(entry, "accent")
			}
		} else {
			entry = fmt.Sprintf("  [%s] [%s] %s", timestamp, roleIndicator, truncatedMsg)
			if cv.styleProvider != nil {
				entry = cv.styleProvider.RenderDimText(entry)
			}
		}

		b.WriteString(entry)
		b.WriteString("\n")
	}

	if end < len(cv.messageSnapshots) {
		moreText := fmt.Sprintf("\n... and %d more messages", len(cv.messageSnapshots)-end)
		if cv.styleProvider != nil {
			b.WriteString(cv.styleProvider.RenderDimText(moreText))
		} else {
			b.WriteString(moreText)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// calculatePaginationBounds calculates the start and end indices for pagination
func (cv *ConversationView) calculatePaginationBounds(maxVisible int) (int, int) {
	totalMessages := len(cv.messageSnapshots)
	if totalMessages <= maxVisible {
		return 0, totalMessages
	}

	start := max(cv.historySelectedIndex-maxVisible/2, 0)
	end := start + maxVisible
	if end > totalMessages {
		end = totalMessages
		start = max(end-maxVisible, 0)
	}

	return start, end
}
