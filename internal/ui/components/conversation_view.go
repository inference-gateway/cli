package components

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	viewport "github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	markdown "github.com/inference-gateway/cli/internal/ui/markdown"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
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
	lineFormatter       *shared.ConversationLineFormatter
	plainTextLines      []string
	configPath          string
	styleProvider       *styles.Provider
	isStreaming         bool
	toolCallRenderer    *ToolCallRenderer
	markdownRenderer    *markdown.Renderer
	rawFormat           bool
}

func NewConversationView(styleProvider *styles.Provider) *ConversationView {
	vp := viewport.New(80, 20)
	vp.SetContent("")

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
		lineFormatter:       shared.NewConversationLineFormatter(80, nil),
		plainTextLines:      []string{},
		styleProvider:       styleProvider,
		markdownRenderer:    mdRenderer,
	}
}

// SetToolFormatter sets the tool formatter for this conversation view
func (cv *ConversationView) SetToolFormatter(formatter domain.ToolFormatter) {
	cv.toolFormatter = formatter
	cv.lineFormatter = shared.NewConversationLineFormatter(cv.width, formatter)
}

// SetConfigPath sets the config path for the welcome message
func (cv *ConversationView) SetConfigPath(configPath string) {
	cv.configPath = configPath
}

// SetToolCallRenderer sets the tool call renderer for displaying real-time tool execution status
func (cv *ConversationView) SetToolCallRenderer(renderer *ToolCallRenderer) {
	cv.toolCallRenderer = renderer
}

func (cv *ConversationView) SetConversation(conversation []domain.ConversationEntry) {
	wasAtBottom := cv.Viewport.AtBottom()
	cv.conversation = conversation
	cv.isStreaming = false
	cv.updatePlainTextLines()
	cv.updateViewportContent()
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

func (cv *ConversationView) ToggleToolResultExpansion(index int) {
	if index >= 0 && index < len(cv.conversation) {
		cv.expandedToolResults[index] = !cv.expandedToolResults[index]
	}
}

func (cv *ConversationView) ToggleAllToolResultsExpansion() {
	cv.allToolsExpanded = !cv.allToolsExpanded

	for i, entry := range cv.conversation {
		if entry.Message.Role == "tool" {
			cv.expandedToolResults[i] = cv.allToolsExpanded
		}
	}
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
	cv.updateViewportContent()
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
	cv.updateViewportContent()
}

// GetPlainTextLines returns the conversation as plain text lines for selection mode
func (cv *ConversationView) GetPlainTextLines() []string {
	return cv.plainTextLines
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
	} else {
		cv.updateViewportContent()
	}

	viewportContent := cv.Viewport.View()
	lines := strings.Split(viewportContent, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func (cv *ConversationView) updateViewportContent() {
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

	wasAtBottom := cv.Viewport.AtBottom()
	cv.Viewport.SetContent(b.String())

	if wasAtBottom {
		cv.Viewport.GotoBottom()
	}
}

func (cv *ConversationView) renderWelcome() string {
	wd, err := os.Getwd()
	if err != nil {
		wd = "unknown"
	}

	statusColor := cv.styleProvider.GetThemeColor("status")
	successColor := cv.styleProvider.GetThemeColor("success")
	dimColor := cv.styleProvider.GetThemeColor("dim")
	headerColor := cv.getHeaderColor()

	headerLine := cv.styleProvider.RenderWithColor("✨ Inference Gateway CLI", statusColor)
	readyLine := cv.styleProvider.RenderWithColor("🚀 Ready to chat!", successColor)
	workingLinePrefix := cv.styleProvider.RenderWithColor("📂 Working in: ", dimColor)
	workingLinePath := cv.styleProvider.RenderWithColor(wd, headerColor)
	workingLine := workingLinePrefix + workingLinePath

	configLine := cv.buildConfigLine()

	content := headerLine + "\n\n" + readyLine + "\n\n" + workingLine + "\n\n" + configLine

	return cv.styleProvider.RenderBorderedBox(content, cv.styleProvider.GetThemeColor("accent"), 1, 1)
}

func (cv *ConversationView) renderEntryWithIndex(entry domain.ConversationEntry, index int) string {
	var color, role string

	switch string(entry.Message.Role) {
	case "user":
		color = cv.getUserColor()
		role = "> You"

		contentStr, contentErr := entry.Message.Content.AsMessageContent0()
		if contentErr == nil && strings.HasPrefix(contentStr, "!") {
			return cv.renderShellCommandEntry(entry, color, role, contentStr)
		}
	case "assistant":
		color = cv.getAssistantColor()
		if entry.Model != "" {
			role = fmt.Sprintf("⏺ %s", entry.Model)
		} else {
			role = "⏺ Assistant"
		}

		if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
			return cv.renderAssistantWithToolCalls(entry, index, color, role)
		}
	case "system":
		color = cv.styleProvider.GetThemeColor("dim")
		role = "⚙️ System"
	case "tool":
		if entry.ToolExecution != nil && !entry.ToolExecution.Success {
			color = cv.styleProvider.GetThemeColor("error")
			role = "🔧 Tool"
		} else if entry.ToolExecution != nil && entry.ToolExecution.Success {
			color = cv.styleProvider.GetThemeColor("success")
			role = "🔧 Tool"
		} else {
			color = cv.styleProvider.GetThemeColor("accent")
			role = "🔧 Tool"
		}
		return cv.renderToolEntry(entry, index, color, role)
	default:
		color = cv.styleProvider.GetThemeColor("dim")
		role = string(entry.Message.Role)
	}

	if entry.Hidden {
		return ""
	}

	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = shared.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}
	content := contentStr

	var formattedContent string
	if entry.Message.Role == sdk.Assistant && cv.markdownRenderer != nil && !cv.isStreaming && !cv.rawFormat {
		formattedContent = cv.markdownRenderer.Render(content)
	} else {
		formattedContent = shared.FormatResponsiveMessage(content, cv.width)
	}

	roleStyled := cv.styleProvider.RenderWithColor(role+":", color)
	message := roleStyled + " " + formattedContent

	return message + "\n"
}

func (cv *ConversationView) renderAssistantWithToolCalls(entry domain.ConversationEntry, _ int, color, role string) string {
	var result strings.Builder

	roleStyled := cv.styleProvider.RenderWithColor(role+":", color)

	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = ""
	}
	if contentStr != "" {
		var formattedContent string
		if cv.markdownRenderer != nil && !cv.rawFormat {
			formattedContent = cv.markdownRenderer.Render(contentStr)
		} else {
			formattedContent = shared.FormatResponsiveMessage(contentStr, cv.width)
		}
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
			helpText = "\nPress ctrl+o to collapse all tool calls"
		}

		return content + helpText
	}
	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = shared.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}
	wrappedContent := shared.FormatResponsiveMessage(contentStr, cv.width)
	return wrappedContent + "\n\n• Press ctrl+o to collapse all tool calls"
}

func (cv *ConversationView) formatCompactContent(entry domain.ConversationEntry) string {
	if entry.ToolExecution != nil {
		content := cv.toolFormatter.FormatToolResultForUI(entry.ToolExecution, cv.width)
		return content + "\n• Press ctrl+o to expand all tool calls"
	}
	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = shared.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}
	content := cv.formatToolContentCompact(contentStr)
	wrappedContent := shared.FormatResponsiveMessage(content, cv.width)
	return wrappedContent + "\n• Press ctrl+o to expand all tool calls"
}

func (cv *ConversationView) formatToolContentCompact(content string) string {
	if cv.toolFormatter == nil {
		lines := strings.Split(content, "\n")
		if len(lines) <= 3 {
			return content
		}
		return strings.Join(lines[:3], "\n") + "\n... (truncated)"
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

	if len(result) <= 3 {
		return strings.Join(result, "\n")
	}
	return strings.Join(result[:3], "\n") + "\n... (truncated)"
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
			case tea.MouseButtonWheelDown:
				cv.Viewport.ScrollDown(1)
				return cv, nil
			case tea.MouseButtonWheelUp:
				cv.Viewport.ScrollUp(1)
				return cv, nil
			}
		}
	}

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		cv.SetWidth(windowMsg.Width)
		cv.height = windowMsg.Height
		cv.updateViewportContent()
	}

	if cv.toolCallRenderer != nil {
		switch msg.(type) {
		case domain.ParallelToolsStartEvent, domain.ToolExecutionProgressEvent:
			if _, rendererCmd := cv.toolCallRenderer.Update(msg); rendererCmd != nil {
				cmd = tea.Batch(cmd, rendererCmd)
			}
			cv.updateViewportContent()
		}
	}

	switch msg := msg.(type) {
	case domain.UpdateHistoryEvent:
		cv.isStreaming = false
		cv.SetConversation(msg.History)
		return cv, cmd
	case domain.StreamingContentEvent:
		cv.appendStreamingContent(msg.Content)
		return cv, cmd
	case domain.ScrollRequestEvent:
		if msg.ComponentID == "conversation" {
			return cv.handleScrollRequest(msg)
		}
	default:
		if _, isKeyMsg := msg.(tea.KeyMsg); !isKeyMsg {
			cv.Viewport, cmd = cv.Viewport.Update(msg)
		}
	}

	return cv, cmd
}

func (cv *ConversationView) handleScrollRequest(msg domain.ScrollRequestEvent) (tea.Model, tea.Cmd) {
	switch msg.Direction {
	case domain.ScrollUp:
		for i := 0; i < msg.Amount; i++ {
			cv.Viewport.ScrollUp(1)
		}
	case domain.ScrollDown:
		for i := 0; i < msg.Amount; i++ {
			cv.Viewport.ScrollDown(1)
		}
	case domain.ScrollToTop:
		cv.Viewport.GotoTop()
	case domain.ScrollToBottom:
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
	wrappedContent := shared.FormatResponsiveMessage(formattedContent, cv.width)

	message := roleStyled + " " + wrappedContent
	return message + "\n"
}

// appendStreamingContent appends streaming content to the last assistant message
func (cv *ConversationView) appendStreamingContent(content string) {
	if !cv.isStreaming {
		cv.isStreaming = true
	}

	if len(cv.conversation) == 0 || cv.conversation[len(cv.conversation)-1].Message.Role != sdk.Assistant {
		streamingEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(content),
			},
			Time: time.Now(),
		}
		cv.conversation = append(cv.conversation, streamingEntry)
	} else {
		lastIdx := len(cv.conversation) - 1
		currentContent, err := cv.conversation[lastIdx].Message.Content.AsMessageContent0()
		if err != nil {
			currentContent = ""
		}
		cv.conversation[lastIdx].Message.Content = sdk.NewMessageContent(currentContent + content)
	}

	cv.updatePlainTextLines()
	cv.updateViewportContent()
}
