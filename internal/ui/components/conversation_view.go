package components

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	viewport "github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
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
	styleProvider       *styles.Provider
	isStreaming         bool
	toolCallRenderer    *ToolCallRenderer
	markdownRenderer    *markdown.Renderer
	rawFormat           bool
	userScrolledUp      bool
	stateManager        domain.StateManager
	renderedContent     string
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

// SetToolCallRenderer sets the tool call renderer for displaying real-time tool execution status
func (cv *ConversationView) SetToolCallRenderer(renderer *ToolCallRenderer) {
	cv.toolCallRenderer = renderer
}

// SetStateManager sets the state manager for accessing plan approval state
func (cv *ConversationView) SetStateManager(stateManager domain.StateManager) {
	cv.stateManager = stateManager
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

// ResetUserScroll resets the user scroll state, enabling auto-scroll to bottom.
// Call this when a new message is sent to ensure the user sees the latest response.
func (cv *ConversationView) ResetUserScroll() {
	cv.userScrolledUp = false
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

	cv.renderedContent = b.String()
	cv.Viewport.SetContent(cv.renderedContent)

	if !cv.userScrolledUp {
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
		if contentErr == nil {
			if strings.HasPrefix(contentStr, "!!") {
				return cv.renderToolCommandEntry(entry, color, role, contentStr)
			} else if strings.HasPrefix(contentStr, "!") {
				return cv.renderShellCommandEntry(entry, color, role, contentStr)
			}
		}
	case "assistant":
		if entry.IsPlan {
			return cv.renderPlanEntry(entry, index)
		}

		if entry.PendingToolCall != nil {
			return cv.renderPendingToolEntry(entry, index)
		}

		if entry.Rejected {
			color = cv.styleProvider.GetThemeColor("dim")
			role = "⊘ Rejected Plan"
		} else {
			color = cv.getAssistantColor()
			if entry.Model != "" {
				role = fmt.Sprintf("⏺ %s", entry.Model)
			} else {
				role = "⏺ Assistant"
			}
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
		contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}
	content := contentStr

	var formattedContent string
	if entry.Message.Role == sdk.Assistant && cv.markdownRenderer != nil && !cv.isStreaming && !cv.rawFormat {
		formattedContent = cv.markdownRenderer.Render(content)
	} else {
		formattedContent = formatting.FormatResponsiveMessage(content, cv.width)
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
			formattedContent = formatting.FormatResponsiveMessage(contentStr, cv.width)
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
		contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}
	wrappedContent := formatting.FormatResponsiveMessage(contentStr, cv.width)
	return wrappedContent + "\n\n• Press ctrl+o to collapse all tool calls"
}

func (cv *ConversationView) formatCompactContent(entry domain.ConversationEntry) string {
	if entry.ToolExecution != nil {
		content := cv.toolFormatter.FormatToolResultForUI(entry.ToolExecution, cv.width)
		return content + "\n• Press ctrl+o to expand all tool calls"
	}
	contentStr, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
	}
	content := cv.formatToolContentCompact(contentStr)
	wrappedContent := formatting.FormatResponsiveMessage(content, cv.width)
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
			case tea.MouseButtonWheelUp:
				cv.userScrolledUp = true
			case tea.MouseButtonWheelDown:
			}
		}
	}

	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		cv.SetWidth(windowMsg.Width)
		cv.height = windowMsg.Height
		cv.updateViewportContent()
	}

	if cv.toolCallRenderer != nil {
		cmd = cv.handleToolCallRendererEvents(msg, cmd)
	}

	switch msg := msg.(type) {
	case domain.UpdateHistoryEvent:
		cv.isStreaming = false
		cv.SetConversation(msg.History)
		return cv, cmd
	case domain.BashCommandCompletedEvent:
		cv.isStreaming = false
		cv.SetConversation(msg.History)
		if cv.toolCallRenderer != nil {
			cv.toolCallRenderer.ClearPreviews()
		}
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
			// After viewport updates, check if we're at bottom
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
	} else if cv.markdownRenderer != nil && !cv.isStreaming && !cv.rawFormat {
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

	// Render buttons with highlighting for selected one
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

	helpText := cv.styleProvider.RenderWithColor(
		"  ←/→: Navigate • Enter: Confirm • Esc: Reject",
		cv.styleProvider.GetThemeColor("dim"),
	)

	return fmt.Sprintf("  %s  %s  %s\n%s", acceptStyled, rejectStyled, autoApproveStyled, helpText)
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

// renderGenericToolArgs renders tool arguments as JSON
func (cv *ConversationView) renderGenericToolArgs(args map[string]any) string {
	argsJSON, _ := json.MarshalIndent(args, "  ", "  ")
	return fmt.Sprintf("  Arguments:\n  %s\n", string(argsJSON))
}

func (cv *ConversationView) renderPendingToolEntry(entry domain.ConversationEntry, index int) string {
	var result strings.Builder

	// Determine the color and role based on approval status
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

	// Format tool call information
	toolCall := entry.PendingToolCall
	toolName := toolCall.Function.Name

	// Parse arguments to display them nicely
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
		result.WriteString(fmt.Sprintf("  Tool: %s\n", toolName))

		switch toolName {
		case "Edit":
			result.WriteString(cv.renderEditToolArgs(args))
		case "Write":
			result.WriteString(cv.renderWriteToolArgs(args))
		default:
			result.WriteString(cv.renderGenericToolArgs(args))
		}
	} else {
		result.WriteString(fmt.Sprintf("  Tool: %s\n", toolName))
	}

	// Render approval buttons if status is pending
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

	helpText := cv.styleProvider.RenderWithColor(
		"  ←/→: Navigate • Enter: Confirm • Esc: Reject",
		cv.styleProvider.GetThemeColor("dim"),
	)

	return fmt.Sprintf("  %s  %s  %s\n%s", approveStyled, rejectStyled, autoApproveStyled, helpText)
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
