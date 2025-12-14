package components

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	viewport "github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	hints "github.com/inference-gateway/cli/internal/ui/hints"
	markdown "github.com/inference-gateway/cli/internal/ui/markdown"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	sdk "github.com/inference-gateway/sdk"
)

// ConversationView handles the chat conversation display
type ConversationView struct {
	conversation        []domain.ConversationEntry
	Viewport            viewport.Model
	width               int
	height              int
	expandedToolResults map[int]bool
	allToolsExpanded    bool
	toolFormatter       domain.ToolFormatter
	lineFormatter       *formatting.ConversationLineFormatter
	plainTextLines      []string
	configPath          string
	versionInfo         *domain.VersionInfo
	styleProvider       *styles.Provider
	toolCallRenderer    *ToolCallRenderer
	markdownRenderer    *markdown.Renderer
	rawFormat           bool
	userScrolledUp      bool
	stateManager        domain.StateManager
	renderedContent     string
	streamingBuffer     strings.Builder
	isStreaming         bool
	streamingModel      string
	keyHintFormatter    *hints.Formatter
}

func NewConversationView(styleProvider *styles.Provider) *ConversationView {
	vp := viewport.New(80, 20)
	vp.SetContent("")
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	var mdRenderer *markdown.Renderer
	if themeService := styleProvider.GetThemeService(); themeService != nil {
		mdRenderer = markdown.NewRenderer(themeService, 80)
	}

	return &ConversationView{
		conversation:        []domain.ConversationEntry{},
		Viewport:            vp,
		width:               80,
		height:              20,
		expandedToolResults: make(map[int]bool),
		allToolsExpanded:    false,
		lineFormatter:       formatting.NewConversationLineFormatter(80, nil),
		plainTextLines:      []string{},
		styleProvider:       styleProvider,
		markdownRenderer:    mdRenderer,
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

func (cv *ConversationView) SetConversation(conversation []domain.ConversationEntry) {
	wasAtBottom := cv.Viewport.AtBottom()
	cv.conversation = conversation
	cv.updatePlainTextLines()
	cv.updateViewportContentFull()
	if wasAtBottom {
		cv.Viewport.GotoBottom()
	}
}

func (cv *ConversationView) GetScrollOffset() int {
	return cv.Viewport.YOffset
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
		cv.expandedToolResults[index] = !cv.expandedToolResults[index]
		cv.updateViewportContentFull()
	}
}

func (cv *ConversationView) ToggleAllToolResultsExpansion() {
	cv.allToolsExpanded = !cv.allToolsExpanded

	for i, entry := range cv.conversation {
		if entry.Message.Role == "tool" {
			cv.expandedToolResults[i] = cv.allToolsExpanded
		}
	}
	cv.updateViewportContentFull()
}

func (cv *ConversationView) IsToolResultExpanded(index int) bool {
	if index >= 0 && index < len(cv.conversation) {
		return cv.expandedToolResults[index]
	}
	return false
}

// ToggleRawFormat toggles between raw and rendered markdown display
func (cv *ConversationView) ToggleRawFormat() {
	cv.rawFormat = !cv.rawFormat
	cv.updateViewportContentFull()
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
	cv.updateViewportContentFull()
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
	cv.Viewport.Width = width
	if cv.lineFormatter != nil {
		cv.lineFormatter.SetWidth(width)
	}
	if cv.markdownRenderer != nil {
		cv.markdownRenderer.SetWidth(width)
	}
}

func (cv *ConversationView) SetHeight(height int) {
	cv.height = height
	cv.Viewport.Height = height
}

func (cv *ConversationView) Render() string {
	if len(cv.conversation) == 0 {
		cv.Viewport.SetContent(cv.renderWelcome())
	}

	viewportContent := cv.Viewport.View()
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
func (cv *ConversationView) appendStreamingContent(content, model string) {
	cv.isStreaming = true
	cv.streamingModel = model
	cv.streamingBuffer.WriteString(content)
	cv.updateViewportContentFull()
}

// flushStreamingBuffer clears the streaming buffer after completion
func (cv *ConversationView) flushStreamingBuffer() {
	cv.streamingBuffer.Reset()
	cv.isStreaming = false
	cv.streamingModel = ""
}

// renderStreamingContent renders the currently streaming assistant message
func (cv *ConversationView) renderStreamingContent() string {
	streamingContent := cv.streamingBuffer.String()

	rolePrefixLength := 13
	if cv.streamingModel != "" {
		rolePrefixLength += len(fmt.Sprintf(" (%s)", cv.streamingModel))
	}

	wrapWidth := cv.width - rolePrefixLength
	if wrapWidth < 40 {
		wrapWidth = 40
	}

	streamingContent = formatting.FormatResponsiveMessage(streamingContent, wrapWidth)

	assistantColor := cv.styleProvider.GetThemeColor("assistant")
	var roleStyled string
	if cv.streamingModel != "" {
		dimColor := cv.styleProvider.GetThemeColor("dim")
		rolePart := cv.styleProvider.RenderWithColor("⏺ Assistant", assistantColor)
		modelLabel := cv.styleProvider.RenderWithColor(fmt.Sprintf(" (%s)", cv.streamingModel), dimColor)
		roleStyled = rolePart + modelLabel + cv.styleProvider.RenderWithColor(":", assistantColor)
	} else {
		roleStyled = cv.styleProvider.RenderWithColor("⏺ Assistant:", assistantColor)
	}

	return roleStyled + " " + streamingContent + "\n"
}

// updateViewportContentFull performs a full rebuild of the viewport content
func (cv *ConversationView) updateViewportContentFull() {
	var b strings.Builder

	displayIndex := 0
	for i, entry := range cv.conversation {
		if entry.Hidden {
			continue
		}
		b.WriteString(cv.renderEntryWithIndex(entry, i))
		b.WriteString("\n")
		displayIndex++
	}

	if cv.toolCallRenderer != nil {
		toolPreviews := cv.toolCallRenderer.RenderPreviews()
		if toolPreviews != "" {
			b.WriteString("\n")
			b.WriteString(toolPreviews)
			b.WriteString("\n")
		}
	}

	if cv.isStreaming && cv.streamingBuffer.Len() > 0 {
		streamingText := cv.renderStreamingContent()
		b.WriteString(streamingText)
	}

	cv.renderedContent = b.String()
	cv.Viewport.SetContent(cv.renderedContent)

	if !cv.userScrolledUp {
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

	headerLine := cv.styleProvider.RenderWithColor("✨ Inference Gateway CLI", statusColor)
	readyLine := cv.styleProvider.RenderWithColor("🚀 Ready to chat!", successColor)

	wd, err := os.Getwd()
	if err != nil {
		wd = "unknown"
	}

	headerColor := cv.getHeaderColor()
	workingLinePrefix := cv.styleProvider.RenderWithColor("📂 Working in: ", dimColor)
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

	headerLine := cv.styleProvider.RenderWithColor("✨ Inference Gateway CLI", statusColor)
	readyLine := cv.styleProvider.RenderWithColor("🚀 Ready to chat!", successColor)
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

func (cv *ConversationView) renderEntryWithIndex(entry domain.ConversationEntry, index int) string {
	if handled, result := cv.tryRenderSpecialEntry(entry, index); handled {
		return result
	}

	color, role := cv.getRoleAndColor(entry)

	if entry.Hidden {
		return ""
	}

	return cv.renderStandardEntry(entry, color, role)
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
			return true, cv.renderPendingToolEntry(entry, index)
		}
		if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
			color, role := cv.getAssistantRoleAndColor(entry)
			return true, cv.renderAssistantWithToolCalls(entry, index, color, role)
		}
	case "tool":
		color, role := cv.getToolRoleAndColor(entry)
		return true, cv.renderToolEntry(entry, index, color, role)
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
func (cv *ConversationView) renderStandardEntry(entry domain.ConversationEntry, color, role string) string {
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

	wrapWidth := cv.width - rolePrefixLength
	if wrapWidth < 40 {
		wrapWidth = 40
	}

	var formattedContent string
	if entry.Message.Role == sdk.Assistant && cv.markdownRenderer != nil && !cv.rawFormat {
		originalWidth := cv.width
		cv.markdownRenderer.SetWidth(wrapWidth)
		formattedContent = cv.markdownRenderer.Render(contentStr)
		cv.markdownRenderer.SetWidth(originalWidth)
	} else {
		formattedContent = formatting.FormatResponsiveMessage(contentStr, wrapWidth)
	}

	roleStyled := cv.formatRoleWithModel(role, color, modelLabelText)
	return roleStyled + " " + formattedContent + "\n"
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

func (cv *ConversationView) renderAssistantWithToolCalls(entry domain.ConversationEntry, _ int, color, role string) string {
	var result strings.Builder

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
		modelLabelLen := 0
		if entry.Model != "" && !entry.Rejected {
			modelLabelLen = len(fmt.Sprintf(" (%s)", entry.Model))
		}
		formattedContent := cv.formatAssistantContent(contentStr, role, modelLabelLen)
		result.WriteString(roleStyled + " " + formattedContent + "\n")
	} else {
		result.WriteString(roleStyled + "\n")
	}

	if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 { // nolint:nestif
		toolCallsColor := cv.styleProvider.GetThemeColor("accent")

		for _, toolCall := range *entry.Message.ToolCalls {
			toolName := toolCall.Function.Name
			toolArgs := toolCall.Function.Arguments

			var argsDisplay string
			if toolArgs != "" && toolArgs != "{}" {
				if len(toolArgs) > 100 {
					argsDisplay = toolArgs[:97] + "..."
				} else {
					argsDisplay = toolArgs
				}
				toolNameStyled := cv.styleProvider.RenderWithColor(toolName, toolCallsColor)
				result.WriteString(fmt.Sprintf("  • %s: %s\n", toolNameStyled, argsDisplay))
			} else {
				toolNameStyled := cv.styleProvider.RenderWithColor(toolName, toolCallsColor)
				result.WriteString(fmt.Sprintf("  • %s\n", toolNameStyled))
			}
		}
	}

	return result.String() + "\n"
}

// formatAssistantContent formats assistant message content with proper wrapping
func (cv *ConversationView) formatAssistantContent(contentStr, role string, modelLabelLen int) string {
	rolePrefixLength := len(role) + 2 + modelLabelLen
	wrapWidth := cv.width - rolePrefixLength
	if wrapWidth < 40 {
		wrapWidth = 40
	}

	if cv.markdownRenderer != nil && !cv.rawFormat {
		originalWidth := cv.width
		cv.markdownRenderer.SetWidth(wrapWidth)
		formattedContent := cv.markdownRenderer.Render(contentStr)
		cv.markdownRenderer.SetWidth(originalWidth)
		return formattedContent
	}

	return formatting.FormatResponsiveMessage(contentStr, wrapWidth)
}

func (cv *ConversationView) renderToolEntry(entry domain.ConversationEntry, index int, color, role string) string {
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

	roleStyled := cv.styleProvider.RenderWithColor(role+":", color)
	message := roleStyled + " " + content
	return message + "\n"
}

func (cv *ConversationView) formatEntryContent(entry domain.ConversationEntry, isExpanded bool) string {
	if isExpanded {
		return cv.formatExpandedContent(entry)
	}
	return cv.formatCompactContent(entry)
}

func (cv *ConversationView) formatExpandedContent(entry domain.ConversationEntry) string {
	if entry.ToolExecution != nil {
		content := cv.toolFormatter.FormatToolResultExpanded(entry.ToolExecution, cv.width)

		var helpText string
		if cv.toolFormatter != nil && cv.toolFormatter.ShouldAlwaysExpandTool(entry.ToolExecution.ToolName) {
			helpText = ""
		} else {
			helpText = "\n• " + cv.getToggleToolHint("collapse all tool calls")
		}

		return content + helpText
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
	hint := cv.getHintForEntry(entry)
	if entry.ToolExecution != nil {
		content := cv.toolFormatter.FormatToolResultForUI(entry.ToolExecution, cv.width)
		return content + "\n• " + hint
	}
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

// buildConfigLine constructs the configuration line for the welcome screen
func (cv *ConversationView) buildConfigLine() string {
	if cv.configPath == "" {
		return ""
	}

	configType := cv.getConfigType()
	displayPath := cv.shortenPath(cv.configPath)

	dimColor := cv.styleProvider.GetThemeColor("dim")
	accentColor := cv.styleProvider.GetThemeColor("accent")

	configPrefix := cv.styleProvider.RenderWithColor("⚙  Config: ", dimColor)
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

	prefix := cv.styleProvider.RenderWithColor("•  Version: ", dimColor)
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

func (cv *ConversationView) View() string { return cv.Render() }

func (cv *ConversationView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		if mouseMsg.Action == tea.MouseActionPress {
			switch mouseMsg.Button {
			case tea.MouseButtonWheelUp:
				cv.userScrolledUp = true
			case tea.MouseButtonWheelDown:
			}
		}
	}

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		cv.SetWidth(windowMsg.Width)
		cv.height = windowMsg.Height
		cv.updateViewportContentFull()
	}

	if cv.toolCallRenderer != nil {
		cmd = cv.handleToolCallRendererEvents(msg, cmd)
	}

	switch msg := msg.(type) {
	case domain.UpdateHistoryEvent:
		cv.flushStreamingBuffer()
		cv.SetConversation(msg.History)
		return cv, cmd
	case domain.BashCommandCompletedEvent:
		cv.SetConversation(msg.History)
		if cv.toolCallRenderer != nil {
			cv.toolCallRenderer.ClearPreviews()
		}
		return cv, cmd
	case domain.StreamingContentEvent:
		cv.appendStreamingContent(msg.Content, msg.Model)
		return cv, cmd
	case domain.ScrollRequestEvent:
		if msg.ComponentID == "conversation" {
			return cv.handleScrollRequest(msg)
		}
	default:
		if _, isKeyMsg := msg.(tea.KeyMsg); !isKeyMsg {
			cv.Viewport, cmd = cv.Viewport.Update(msg)
			if cv.Viewport.AtBottom() {
				cv.userScrolledUp = false
			}
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

// renderPlanEntry renders a plan entry with inline approval buttons
func (cv *ConversationView) renderPlanEntry(entry domain.ConversationEntry, index int) string {
	var result strings.Builder

	var color string
	var role string
	switch entry.PlanApprovalStatus {
	case domain.PlanApprovalPending:
		color = cv.styleProvider.GetThemeColor("accent")
		role = "Plan (Pending Approval)"
	case domain.PlanApprovalAccepted:
		color = cv.styleProvider.GetThemeColor("success")
		role = "Plan (Accepted)"
	case domain.PlanApprovalRejected:
		color = cv.styleProvider.GetThemeColor("dim")
		role = "Plan (Rejected)"
	default:
		color = cv.getAssistantColor()
		role = "Plan"
	}

	roleStyled := cv.styleProvider.RenderWithColor(role+":", color)

	// Render the plan content
	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}

	var formattedContent string

	if entry.PlanApprovalStatus == domain.PlanApprovalRejected {
		plainContent := formatting.FormatResponsiveMessage(contentStr, cv.width)
		formattedContent = cv.styleProvider.RenderWithColor(plainContent, color)
	} else if cv.markdownRenderer != nil && !cv.rawFormat {
		formattedContent = cv.markdownRenderer.Render(contentStr)
	} else {
		formattedContent = formatting.FormatResponsiveMessage(contentStr, cv.width)
	}

	result.WriteString(roleStyled + " " + formattedContent + "\n")

	if entry.PlanApprovalStatus == domain.PlanApprovalPending {
		result.WriteString("\n")
		result.WriteString(cv.renderInlineApprovalButtons(index))
	}

	return result.String() + "\n"
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
	autoApproveText := "Auto-Approve"

	successColor := cv.styleProvider.GetThemeColor("success")
	errorColor := cv.styleProvider.GetThemeColor("error")
	accentColor := cv.styleProvider.GetThemeColor("accent")
	highlightBg := cv.styleProvider.GetThemeColor("selection_bg")

	var acceptStyled, rejectStyled, autoApproveStyled string
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

	if selectedIndex == int(domain.PlanApprovalAcceptAndAutoApprove) {
		autoApproveStyled = cv.styleProvider.RenderStyledText("[ "+autoApproveText+" ]", styles.StyleOptions{
			Foreground: accentColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		autoApproveStyled = cv.styleProvider.RenderWithColor("[ "+autoApproveText+" ]", accentColor)
	}

	return fmt.Sprintf("  %s  %s  %s", acceptStyled, rejectStyled, autoApproveStyled)
}

// renderPendingToolEntry renders a pending tool call that requires approval
// renderEditToolArgs renders the Edit tool arguments with a diff
func (cv *ConversationView) renderEditToolArgs(args map[string]any) string {
	var result strings.Builder

	oldStr, hasOld := args["old_string"].(string)
	newStr, hasNew := args["new_string"].(string)
	filePath, hasPath := args["file_path"].(string)

	if hasOld && hasNew && hasPath {
		result.WriteString(fmt.Sprintf("  File: %s\n\n", filePath))
		diffRenderer := NewDiffRenderer(cv.styleProvider)
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
		result.WriteString(fmt.Sprintf("  File: %s\n", filePath))
	}
	if content, ok := args["content"].(string); ok {
		preview := content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		result.WriteString(fmt.Sprintf("  Content: %s\n", preview))
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

	lines := strings.Split(rendered, "\n")
	for _, line := range lines {
		if line != "" {
			result.WriteString("    " + line + "\n")
		} else {
			result.WriteString("\n")
		}
	}
}

// renderGenericToolArgs renders tool arguments as JSON
func (cv *ConversationView) renderGenericToolArgs(args map[string]any) string {
	argsJSON, _ := json.MarshalIndent(args, "  ", "  ")
	return fmt.Sprintf("  Arguments:\n  %s\n", string(argsJSON))
}

func (cv *ConversationView) renderPendingToolEntry(entry domain.ConversationEntry, index int) string {
	var result strings.Builder

	var color string
	var role string
	switch entry.ToolApprovalStatus {
	case domain.ToolApprovalPending:
		color = cv.styleProvider.GetThemeColor("accent")
		role = "Tool (Pending Approval)"
	case domain.ToolApprovalApproved:
		color = cv.styleProvider.GetThemeColor("success")
		role = "Tool (Approved)"
	case domain.ToolApprovalRejected:
		color = cv.styleProvider.GetThemeColor("error")
		role = "Tool (Rejected)"
	default:
		color = cv.getAssistantColor()
		role = "Tool"
	}

	roleStyled := cv.styleProvider.RenderWithColor(role+":", color)
	result.WriteString(roleStyled + "\n")

	toolCall := entry.PendingToolCall
	toolName := toolCall.Function.Name

	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
		result.WriteString(fmt.Sprintf("  Tool: %s\n", toolName))

		switch toolName {
		case "Edit":
			result.WriteString(cv.renderEditToolArgs(args))
		case "Write":
			result.WriteString(cv.renderWriteToolArgs(args))
		case "RequestPlanApproval":
			result.WriteString(cv.renderRequestPlanApprovalArgs(args))
		default:
			result.WriteString(cv.renderGenericToolArgs(args))
		}
	} else {
		result.WriteString(fmt.Sprintf("  Tool: %s\n", toolName))
	}

	if entry.ToolApprovalStatus == domain.ToolApprovalPending {
		result.WriteString("\n")
		result.WriteString(cv.renderInlineToolApprovalButtons(index))
	}

	return result.String() + "\n"
}

// renderInlineToolApprovalButtons renders inline approval buttons for a tool
func (cv *ConversationView) renderInlineToolApprovalButtons(_ int) string {
	selectedIndex := 0
	if cv.stateManager != nil {
		if approvalState := cv.stateManager.GetApprovalUIState(); approvalState != nil {
			selectedIndex = approvalState.SelectedIndex
		}
	}

	approveText := "Approve"
	rejectText := "Reject"
	autoApproveText := "Auto-Approve"

	successColor := cv.styleProvider.GetThemeColor("success")
	errorColor := cv.styleProvider.GetThemeColor("error")
	accentColor := cv.styleProvider.GetThemeColor("accent")
	highlightBg := cv.styleProvider.GetThemeColor("selection_bg")

	// Render buttons with highlighting for selected one
	var approveStyled, rejectStyled, autoApproveStyled string
	if selectedIndex == int(domain.ApprovalApprove) {
		approveStyled = cv.styleProvider.RenderStyledText("[ "+approveText+" ]", styles.StyleOptions{
			Foreground: successColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		approveStyled = cv.styleProvider.RenderWithColor("[ "+approveText+" ]", successColor)
	}

	if selectedIndex == int(domain.ApprovalReject) {
		rejectStyled = cv.styleProvider.RenderStyledText("[ "+rejectText+" ]", styles.StyleOptions{
			Foreground: errorColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		rejectStyled = cv.styleProvider.RenderWithColor("[ "+rejectText+" ]", errorColor)
	}

	if selectedIndex == int(domain.ApprovalAutoAccept) {
		autoApproveStyled = cv.styleProvider.RenderStyledText("[ "+autoApproveText+" ]", styles.StyleOptions{
			Foreground: accentColor,
			Background: highlightBg,
			Bold:       true,
		})
	} else {
		autoApproveStyled = cv.styleProvider.RenderWithColor("[ "+autoApproveText+" ]", accentColor)
	}

	return fmt.Sprintf("  %s  %s  %s", approveStyled, rejectStyled, autoApproveStyled)
}

// handleToolCallRendererEvents processes tool call renderer specific events
func (cv *ConversationView) handleToolCallRendererEvents(msg tea.Msg, cmd tea.Cmd) tea.Cmd {
	switch msg := msg.(type) {
	case domain.ParallelToolsStartEvent:
		if _, rendererCmd := cv.toolCallRenderer.Update(msg); rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.ToolExecutionProgressEvent:
		if _, rendererCmd := cv.toolCallRenderer.Update(msg); rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.BashOutputChunkEvent:
		if _, rendererCmd := cv.toolCallRenderer.Update(msg); rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.ParallelToolsCompleteEvent:
		if _, rendererCmd := cv.toolCallRenderer.Update(msg); rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	case domain.ChatCompleteEvent:
		if _, rendererCmd := cv.toolCallRenderer.Update(msg); rendererCmd != nil {
			cmd = tea.Batch(cmd, rendererCmd)
		}
	}

	cv.updateViewportContent()
	return cmd
}

// getHintForEntry returns the appropriate hint based on entry state
func (cv *ConversationView) getHintForEntry(_ domain.ConversationEntry) string {
	return cv.getToggleToolHint("expand all tool calls")
}

func (cv *ConversationView) getToggleToolHint(action string) string {
	if cv.keyHintFormatter == nil {
		return "Press ctrl+o to " + action
	}

	actionID := config.ActionID(config.NamespaceTools, "toggle_tool_expansion")
	hint := cv.keyHintFormatter.GetKeyHint(actionID, action)
	if hint == "" {
		return "Press ctrl+o to " + action
	}

	return hint
}
