package components

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	spinner "charm.land/bubbles/v2/spinner"
	viewport "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	hints "github.com/inference-gateway/cli/internal/presentation/tui/hints"
	markdown "github.com/inference-gateway/cli/internal/presentation/tui/markdown"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
)

// NavigationMode represents the current navigation state of the conversation view
type NavigationMode int

const (
	// NavigationModeNormal is the default mode for displaying conversation
	NavigationModeNormal NavigationMode = iota
	// NavigationModeMessageHistory is the mode for navigating message history
	NavigationModeMessageHistory
)

// ConversationView handles the chat conversation display. It is confined to
// the Bubble Tea event loop: all state is read and written from Update/View
// only, so it holds no locks. Off-loop producers must go through Program.Send.
type ConversationView struct {
	conversation           []convdomain.ConversationEntry
	Viewport               viewport.Model
	width                  int
	height                 int
	expandedToolResults    map[int]bool
	expandedThinkingBlocks map[int]bool
	allToolsExpanded       bool
	allThinkingExpanded    bool
	defaultExpandedTools   map[string]bool
	toolFormatter          agentdomain.ToolFormatter
	lineFormatter          *formatting.ConversationLineFormatter
	configPath             string
	versionInfo            *tui.VersionInfo
	styleProvider          *styles.Provider
	toolCallRenderer       *ToolCallRenderer
	markdownRenderer       *markdown.Renderer
	rawFormat              bool
	stateManager           agentdomain.PlanApprovalUIManager
	renderedContent        string

	// renderCache memoizes per-entry rendered output keyed by conversation
	// index; an entry re-renders only when its fingerprint changes. Cleared
	// on theme refresh, which restyles without touching entry state.
	renderCache map[int]renderCacheEntry

	// Streaming state
	streamingBuffer          strings.Builder
	streamingReasoningBuffer strings.Builder
	isStreaming              bool
	streamingModel           string
	streamingDirty           bool
	streamingRenderArmed     bool

	keyHintFormatter *hints.Formatter

	// Message history navigation
	navigationMode       NavigationMode
	messageSnapshots     []tui.MessageSnapshot
	historySelectedIndex int
}

func NewConversationView(styleProvider *styles.Provider) *ConversationView {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent("")
	// Keyboard scrolling is routed through the configurable keybindings
	// (ScrollRequestEvent); the viewport only owns wheel input.
	vp.KeyMap = viewport.KeyMap{}
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	var mdRenderer *markdown.Renderer
	if themeService := styleProvider.GetThemeService(); themeService != nil {
		mdRenderer = markdown.NewRenderer(themeService, 80)
	}

	return &ConversationView{
		conversation:           []convdomain.ConversationEntry{},
		Viewport:               vp,
		width:                  80,
		height:                 20,
		expandedToolResults:    make(map[int]bool),
		expandedThinkingBlocks: make(map[int]bool),
		allToolsExpanded:       false,
		allThinkingExpanded:    false,
		defaultExpandedTools:   map[string]bool{"Edit": true, "MultiEdit": true},
		lineFormatter:          formatting.NewConversationLineFormatter(80, nil),
		styleProvider:          styleProvider,
		markdownRenderer:       mdRenderer,
		renderCache:            make(map[int]renderCacheEntry),
	}
}

// SetToolFormatter sets the tool formatter for this conversation view
func (cv *ConversationView) SetToolFormatter(formatter agentdomain.ToolFormatter) {
	cv.toolFormatter = formatter
	cv.lineFormatter = formatting.NewConversationLineFormatter(cv.width, formatter)
}

// SetConfigPath sets the config path for the welcome message
func (cv *ConversationView) SetConfigPath(configPath string) {
	cv.configPath = configPath
}

// SetVersionInfo sets the version information for the welcome message
func (cv *ConversationView) SetVersionInfo(info tui.VersionInfo) {
	cv.versionInfo = &info
}

// SetToolCallRenderer sets the tool call renderer for displaying real-time tool execution status
func (cv *ConversationView) SetToolCallRenderer(renderer *ToolCallRenderer) {
	cv.toolCallRenderer = renderer
}

// SetStateManager sets the state manager for accessing plan approval state
func (cv *ConversationView) SetStateManager(stateManager agentdomain.PlanApprovalUIManager) {
	cv.stateManager = stateManager
}

// SetKeyHintFormatter sets the key hint formatter for displaying keybinding hints
func (cv *ConversationView) SetKeyHintFormatter(formatter *hints.Formatter) {
	cv.keyHintFormatter = formatter
}

func (cv *ConversationView) SetConversation(conversation []convdomain.ConversationEntry) {
	if len(conversation) < len(cv.conversation) {
		cv.renderCache = make(map[int]renderCacheEntry)
	}
	cv.conversation = conversation

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContentFull()
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

// ResetUserScroll pins the viewport to the tail so it follows new content.
// Call this when a new message is sent to ensure the user sees the latest response.
func (cv *ConversationView) ResetUserScroll() {
	cv.Viewport.GotoBottom()
}

func (cv *ConversationView) ToggleToolResultExpansion(index int) {
	if index >= 0 && index < len(cv.conversation) {
		cv.rebuildPreservingScroll(func() {
			cv.expandedToolResults[index] = !cv.IsToolResultExpanded(index)
		}, index)
	}
}

func (cv *ConversationView) ToggleAllToolResultsExpansion() {
	expand := !cv.allToolResultsExpanded()

	changed := make([]int, 0, len(cv.conversation))
	for i, entry := range cv.conversation {
		if entry.Message.Role == "tool" {
			changed = append(changed, i)
		}
	}

	cv.rebuildPreservingScroll(func() {
		cv.allToolsExpanded = expand
		for _, i := range changed {
			cv.expandedToolResults[i] = expand
		}
	}, changed...)
}

// rebuildPreservingScroll applies mutate (the expand/collapse state change) and
// re-renders the viewport while keeping the content the user is looking at anchored
// in place. Expanding an entry that sits above the viewport top would otherwise shift
// everything down and make the view jump; we offset by the height delta of the changed
// entries above the current scroll position. The old layout is measured before mutate
// runs, so the delta is real. When the user is following the tail we stay pinned to the
// bottom.
func (cv *ConversationView) rebuildPreservingScroll(mutate func(), changed ...int) {
	if cv.navigationMode == NavigationModeMessageHistory {
		mutate()
		return
	}
	if cv.Viewport.AtBottom() {
		mutate()
		cv.updateViewportContentFull()
		return
	}

	oldOffset := cv.Viewport.YOffset()
	oldSpans := cv.entryLineSpans()

	mutate()
	cv.updateViewportContentFull()

	newSpans := cv.entryLineSpans()
	delta := 0
	for _, idx := range changed {
		old, ok := oldSpans[idx]
		if !ok || old[0] >= oldOffset {
			continue
		}
		if nw, ok := newSpans[idx]; ok {
			delta += nw[1] - old[1]
		}
	}
	if delta != 0 {
		cv.Viewport.SetYOffset(oldOffset + delta)
	}
}

// entryLineSpans returns, per visible conversation entry, its {startLine, height}
// in the rendered viewport content - the same coordinate space YOffset uses. Heights
// come from the same cached per-entry render updateViewportContentFull emits, so the
// two stay in lockstep.
func (cv *ConversationView) entryLineSpans() map[int][2]int {
	spans := make(map[int][2]int, len(cv.conversation))
	line := 0
	for i, entry := range cv.conversation {
		if entry.Hidden {
			continue
		}
		h := cv.styleProvider.GetHeight(cv.renderEntryCached(entry, i))
		spans[i] = [2]int{line, h}
		line += h
	}
	return spans
}

// allToolResultsExpanded reports whether every tool result is currently expanded
// (honoring per-tool defaults), so the global toggle collapses only when there is
// nothing left to expand. False when there are no tool results, which makes the
// first press an expand no-op rather than a surprising collapse.
func (cv *ConversationView) allToolResultsExpanded() bool {
	found := false
	for i, entry := range cv.conversation {
		if entry.Message.Role != "tool" {
			continue
		}
		found = true
		if !cv.IsToolResultExpanded(i) {
			return false
		}
	}
	return found
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
	changed := make([]int, 0, len(cv.conversation))
	for i, entry := range cv.conversation {
		if entry.ReasoningContent != "" {
			changed = append(changed, i)
		}
	}

	cv.rebuildPreservingScroll(func() {
		cv.allThinkingExpanded = !cv.allThinkingExpanded
		for _, i := range changed {
			cv.expandedThinkingBlocks[i] = cv.allThinkingExpanded
		}
	}, changed...)
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

// SetWidth resizes the view and rewraps content; a no-op when unchanged.
func (cv *ConversationView) SetWidth(width int) {
	if width == cv.width {
		return
	}
	cv.width = width
	cv.Viewport.SetWidth(width)
	if cv.lineFormatter != nil {
		cv.lineFormatter.SetWidth(width)
	}
	if cv.markdownRenderer != nil {
		cv.markdownRenderer.SetWidth(width)
	}
	if cv.toolCallRenderer != nil {
		cv.toolCallRenderer.SetWidth(width)
	}
	cv.rebuild()
}

// SetHeight resizes the view; a no-op when unchanged.
func (cv *ConversationView) SetHeight(height int) {
	if height == cv.height {
		return
	}
	cv.height = height
	cv.Viewport.SetHeight(height)
	cv.rebuild()
}

// rebuild re-renders whichever view is active after a size change.
func (cv *ConversationView) rebuild() {
	if cv.navigationMode == NavigationModeMessageHistory {
		cv.updateMessageHistoryView()
		return
	}
	cv.updateViewportContentFull()
}

// Render returns the viewport with a two-space left gutter. It never mutates
// the viewport; sizing and content updates happen in Update/SetWidth/SetHeight.
func (cv *ConversationView) Render() string {
	content := cv.Viewport.View()
	if len(cv.conversation) == 0 && cv.navigationMode != NavigationModeMessageHistory {
		content = cv.renderWelcome()
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = "  " + strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func (cv *ConversationView) updateViewportContent() {
	cv.updateViewportContentFull()
}

// streamingRenderInterval bounds how often the viewport is rebuilt while an
// assistant message streams. Deltas arrive far faster than this (a real model
// emits many tokens/sec); rebuilding + SetContent + GotoBottom on every delta
// hands the 60fps renderer a fully-reflowed frame per token, which the terminal
// cannot paint cleanly and shows as mid-stream scrambling (issue #888). We
// coalesce to ~30fps: visually live, but at most one rebuild per tick.
const streamingRenderInterval = 33 * time.Millisecond

// streamingRenderTickMsg drives the coalesced streaming re-render loop.
type streamingRenderTickMsg struct{}

func streamingRenderTick() tea.Cmd {
	return tea.Tick(streamingRenderInterval, func(time.Time) tea.Msg { return streamingRenderTickMsg{} })
}

// appendStreamingContent appends a streamed delta and marks the view dirty; the
// actual rebuild is deferred to the coalescing tick (handleStreamingRenderTick),
// so a burst of tokens costs one render per tick instead of one render each.
func (cv *ConversationView) appendStreamingContent(content, reasoning, model string) {
	cv.isStreaming = true
	cv.streamingModel = model
	cv.streamingBuffer.WriteString(content)
	cv.streamingReasoningBuffer.WriteString(reasoning)
	cv.streamingDirty = true
}

// flushStreamingBuffer clears the streaming buffer after completion. isStreaming
// flips false so the coalescing render tick stops re-arming on its next fire.
func (cv *ConversationView) flushStreamingBuffer() {
	cv.streamingBuffer.Reset()
	cv.streamingReasoningBuffer.Reset()
	cv.isStreaming = false
	cv.streamingModel = ""
	cv.streamingDirty = false
}

// renderStreamingContent renders the currently streaming assistant message
func (cv *ConversationView) renderStreamingContent() string {
	streamingContent := cv.streamingBuffer.String()
	streamingReasoning := cv.streamingReasoningBuffer.String()
	model := cv.streamingModel

	var result strings.Builder

	if streamingReasoning != "" {
		isExpanded := cv.allThinkingExpanded || cv.expandedThinkingBlocks[-1]
		thinkingBlock := cv.renderThinkingBlock(streamingReasoning, -1, isExpanded)
		result.WriteString(thinkingBlock)
	}

	streamingContent = cv.applyMarkdownIfEnabled(streamingContent, max(cv.width-2, 40))

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

	cv.writeRoleAndBody(&result, roleStyled, streamingContent)
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

	shouldRenderStreaming := cv.isStreaming && (cv.streamingBuffer.Len() > 0 || cv.streamingReasoningBuffer.Len() > 0)
	if shouldRenderStreaming {
		streamingText := cv.renderStreamingContent()
		b.WriteString(streamingText)
	}

	cv.renderedContent = b.String()

	atBottom := cv.Viewport.AtBottom()
	cv.Viewport.SetContent(cv.renderedContent)
	if atBottom {
		cv.Viewport.GotoBottom()
	}
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
func (cv *ConversationView) renderEntryCached(entry convdomain.ConversationEntry, index int) string {
	if entry.IsPlan && entry.PlanApprovalStatus == convdomain.PlanApprovalPending {
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
func (cv *ConversationView) entryFingerprint(entry convdomain.ConversationEntry, index int) uint64 {
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

func (cv *ConversationView) renderEntryWithIndex(entry convdomain.ConversationEntry, index int) string {
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
func (cv *ConversationView) tryRenderSpecialEntry(entry convdomain.ConversationEntry, index int) (bool, string) {
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
func (cv *ConversationView) tryRenderUserCommand(entry convdomain.ConversationEntry) string {
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
func (cv *ConversationView) getRoleAndColor(entry convdomain.ConversationEntry) (string, string) {
	switch string(entry.Message.Role) {
	case "user":
		return cv.getUserColor(), "> You"
	case "assistant":
		return cv.getAssistantRoleAndColor(entry)
	case "system":
		return cv.styleProvider.GetThemeColor("dim"), "System"
	case "tool":
		return cv.getToolRoleAndColor(entry)
	default:
		return cv.styleProvider.GetThemeColor("dim"), string(entry.Message.Role)
	}
}

// getAssistantRoleAndColor returns role and color for assistant entries
func (cv *ConversationView) getAssistantRoleAndColor(entry convdomain.ConversationEntry) (string, string) {
	if entry.Rejected {
		return cv.styleProvider.GetThemeColor("dim"), "⊘ Rejected Plan"
	}
	return cv.getAssistantColor(), "⏺ Assistant"
}

// getToolRoleAndColor returns role and color for tool entries
func (cv *ConversationView) getToolRoleAndColor(entry convdomain.ConversationEntry) (string, string) {
	role := "Tool"
	if entry.ToolExecution != nil && !entry.ToolExecution.Success {
		return cv.styleProvider.GetThemeColor("error"), role
	}
	if entry.ToolExecution != nil && entry.ToolExecution.Success {
		return cv.styleProvider.GetThemeColor("success"), role
	}
	return cv.styleProvider.GetThemeColor("accent"), role
}

// renderStandardEntry renders a standard message entry
func (cv *ConversationView) renderStandardEntry(entry convdomain.ConversationEntry, index int, color, role string) string {
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
func (cv *ConversationView) renderInlineContent(result *strings.Builder, roleStyled string, entry convdomain.ConversationEntry, contentStr string, wrapWidth int) {
	if entry.Message.Role == sdk.Assistant && cv.markdownRenderer != nil && !cv.rawFormat {
		body := cv.applyMarkdownIfEnabled(contentStr, max(cv.width-2, 40))
		cv.writeRoleAndBody(result, roleStyled, body)
		return
	}
	formattedContent := formatting.FormatResponsiveMessage(contentStr, wrapWidth)
	result.WriteString(roleStyled)
	result.WriteString(" ")
	result.WriteString(formattedContent)
	result.WriteString("\n")
}

// writeRoleAndBody joins a role prefix and an already-wrapped body. The body wraps
// to the full width, so its first line is kept on the role line only when it still
// fits after the prefix; otherwise the body starts on its own line to avoid clipping.
func (cv *ConversationView) writeRoleAndBody(result *strings.Builder, roleStyled, body string) {
	result.WriteString(roleStyled)
	firstLine, _, _ := strings.Cut(body, "\n")
	if lipgloss.Width(roleStyled)+1+lipgloss.Width(firstLine) <= cv.width {
		result.WriteString(" ")
	} else {
		result.WriteString("\n")
	}
	result.WriteString(body)
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

func (cv *ConversationView) renderAssistantWithToolCalls(entry convdomain.ConversationEntry, index int, color, role string) string {
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

func (cv *ConversationView) renderToolEntry(entry convdomain.ConversationEntry, index int) string {
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

func (cv *ConversationView) formatEntryContent(entry convdomain.ConversationEntry, isExpanded bool) string {
	if isExpanded {
		return cv.formatExpandedContent(entry)
	}
	return cv.formatCompactContent(entry)
}

func (cv *ConversationView) formatExpandedContent(entry convdomain.ConversationEntry) string {
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

func (cv *ConversationView) formatCompactContent(entry convdomain.ConversationEntry) string {
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

	switch msg := msg.(type) {
	case tui.PlanApprovalSelectionChangedEvent:
		return cv.handlePlanApprovalSelectionChanged(msg, cmd)
	case tui.UpdateHistoryEvent:
		return cv.handleUpdateHistoryEvent(msg, cmd)
	case agentdomain.ToolCallPreviewEvent, agentdomain.ToolExecutionProgressEvent, agentdomain.BashOutputChunkEvent, agentdomain.ChatCompleteEvent:
		return cv.handleToolCallEvents(msg, cmd)
	case tui.BashCommandCompletedEvent:
		return cv.handleBashCommandCompletedEvent(msg, cmd)
	case agentdomain.ChatStartEvent:
		return cv.handleChatStartEvent(cmd)
	case tui.StreamingContentEvent:
		return cv.handleStreamingContentEvent(msg, cmd)
	case tui.ScrollRequestEvent:
		return cv.handleScrollRequestEvent(msg, cmd)
	case spinner.TickMsg:
		return cv.handleSpinnerTick(msg, cmd)
	case streamingRenderTickMsg:
		return cv.handleStreamingRenderTick(cmd)
	default:
		return cv.handleDefaultEvents(msg)
	}
}

// handlePlanApprovalSelectionChanged refreshes the conversation viewport so
// the highlighted plan-approval button reflects the new selection index.
func (cv *ConversationView) handlePlanApprovalSelectionChanged(_ tui.PlanApprovalSelectionChangedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cv, cmd
}

// handleUpdateHistoryEvent processes history update events
func (cv *ConversationView) handleUpdateHistoryEvent(msg tui.UpdateHistoryEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
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
func (cv *ConversationView) handleBashCommandCompletedEvent(msg tui.BashCommandCompletedEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.SetConversation(msg.History)
		if cv.toolCallRenderer != nil {
			cv.toolCallRenderer.ClearPreviews()
		}
	}
	return cv, cmd
}

// handleChatStartEvent clears any leftover streaming content when a new
// stream begins - after a mid-stream reconnect the retried response restarts
// from the top, so the partial output of the broken attempt must not remain.
func (cv *ConversationView) handleChatStartEvent(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.flushStreamingBuffer()
		cv.updateViewportContentFull()
	}
	return cv, cmd
}

// handleStreamingContentEvent processes streaming content events
func (cv *ConversationView) handleStreamingContentEvent(msg tui.StreamingContentEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.navigationMode != NavigationModeMessageHistory {
		cv.appendStreamingContent(msg.Content, msg.ReasoningContent, msg.Model)
		if !cv.streamingRenderArmed {
			cv.streamingRenderArmed = true
			return cv, tea.Batch(cmd, streamingRenderTick())
		}
	}
	return cv, cmd
}

// handleStreamingRenderTick performs the coalesced viewport rebuild: at most one
// rebuild per tick while streaming, re-arming until streaming ends (issue #888).
func (cv *ConversationView) handleStreamingRenderTick(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.streamingDirty {
		cv.streamingDirty = false
		cv.updateViewportContentFull()
	}
	if cv.isStreaming {
		return cv, tea.Batch(cmd, streamingRenderTick())
	}
	cv.streamingRenderArmed = false
	return cv, cmd
}

// handleScrollRequestEvent processes scroll request events
func (cv *ConversationView) handleScrollRequestEvent(msg tui.ScrollRequestEvent, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.ComponentID == "conversation" {
		return cv.handleScrollRequest(msg)
	}
	return cv, cmd
}

// handleSpinnerTick forwards spinner ticks to the ToolCallRenderer and
// repaints while it has live previews.
func (cv *ConversationView) handleSpinnerTick(msg spinner.TickMsg, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cv.toolCallRenderer == nil {
		return cv, cmd
	}
	updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
	cv.toolCallRenderer = updatedRenderer
	if cv.navigationMode != NavigationModeMessageHistory && cv.toolCallRenderer.HasActivePreviews() {
		cv.updateViewportContent()
	}
	if rendererCmd != nil {
		cmd = tea.Batch(cmd, rendererCmd)
	}
	return cv, cmd
}

// handleDefaultEvents processes all other events
func (cv *ConversationView) handleDefaultEvents(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	cv.Viewport, cmd = cv.Viewport.Update(msg)
	return cv, cmd
}

func (cv *ConversationView) handleScrollRequest(msg tui.ScrollRequestEvent) (tea.Model, tea.Cmd) {
	switch msg.Direction {
	case tui.ScrollUp:
		cv.Viewport.ScrollUp(msg.Amount)
	case tui.ScrollDown:
		cv.Viewport.ScrollDown(msg.Amount)
	case tui.ScrollToTop:
		cv.Viewport.GotoTop()
	case tui.ScrollToBottom:
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
func (cv *ConversationView) renderShellCommandEntry(_ convdomain.ConversationEntry, color, role, contentStr string) string {
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
func (cv *ConversationView) renderToolCommandEntry(_ convdomain.ConversationEntry, color, role, contentStr string) string {
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
func (cv *ConversationView) renderPlanEntry(entry convdomain.ConversationEntry, index int) string {
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
	case convdomain.PlanApprovalRejected:
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

	if entry.PlanApprovalStatus == convdomain.PlanApprovalPending {
		result.WriteString("\n")
		result.WriteString(cv.renderInlineApprovalButtons(index))
		result.WriteString("\n")
	}

	return result.String() + "\n"
}

// planRoleAndColor returns the role label + theme color for a plan entry
// based on its approval status.
func (cv *ConversationView) planRoleAndColor(entry convdomain.ConversationEntry) (string, string) {
	switch entry.PlanApprovalStatus {
	case convdomain.PlanApprovalPending:
		return cv.styleProvider.GetThemeColor("accent"), "Plan (Pending Approval)"
	case convdomain.PlanApprovalAccepted:
		return cv.styleProvider.GetThemeColor("success"), "Plan (Accepted)"
	case convdomain.PlanApprovalRejected:
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
	if selectedIndex == int(agentdomain.PlanApprovalAccept) {
		acceptStyled = cv.styleProvider.RenderStyledText("[ "+acceptText+" ]", styles.StyleOptions{
			Foreground: successColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		acceptStyled = cv.styleProvider.RenderWithColor("[ "+acceptText+" ]", successColor)
	}

	if selectedIndex == int(agentdomain.PlanApprovalReject) {
		rejectStyled = cv.styleProvider.RenderStyledText("[ "+rejectText+" ]", styles.StyleOptions{
			Foreground: errorColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		rejectStyled = cv.styleProvider.RenderWithColor("[ "+rejectText+" ]", errorColor)
	}

	if selectedIndex == int(agentdomain.PlanApprovalAcceptStandard) {
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
		diffRenderer := styles.NewDiffRenderer(cv.styleProvider).SetContextLines(styles.InlineDiffContextLines)
		diffRenderer.SetStartLine(styles.SnippetStartLine(filePath, oldStr))
		diffInfo := styles.DiffInfo{
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

func (cv *ConversationView) renderPendingToolEntry(entry convdomain.ConversationEntry) string {
	if entry.ToolApprovalStatus == convdomain.ToolApprovalPending {
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
func (cv *ConversationView) renderApprovalHeader(toolName string, args map[string]any, status convdomain.ToolApprovalStatus) string {
	icon := icons.CheckMark
	colorName := "success"
	label := "Approved"
	if status == convdomain.ToolApprovalRejected {
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

// handleToolCallRendererEvents forwards a tool-call event (preview, progress,
// bash output, chat complete) to the ToolCallRenderer and repaints.
func (cv *ConversationView) handleToolCallRendererEvents(msg tea.Msg, cmd tea.Cmd) tea.Cmd {
	updatedRenderer, rendererCmd := cv.toolCallRenderer.Update(msg)
	cv.toolCallRenderer = updatedRenderer
	if rendererCmd != nil {
		cmd = tea.Batch(cmd, rendererCmd)
	}

	if cv.navigationMode != NavigationModeMessageHistory {
		cv.updateViewportContent()
	}
	return cmd
}

// getHintForEntry returns the appropriate hint based on entry state
func (cv *ConversationView) getHintForEntry(_ convdomain.ConversationEntry) string {
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
func (cv *ConversationView) EnterMessageHistoryMode(snapshots []tui.MessageSnapshot) {
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
func (cv *ConversationView) GetSelectedMessageSnapshot() *tui.MessageSnapshot {
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
