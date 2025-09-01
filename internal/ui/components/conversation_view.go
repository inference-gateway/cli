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
	lipgloss "github.com/charmbracelet/lipgloss"
	domain "github.com/inference-gateway/cli/internal/domain"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
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
	themeService        domain.ThemeService
	isStreaming         bool
}

func NewConversationView(themeService domain.ThemeService) *ConversationView {
	vp := viewport.New(80, 20)
	vp.SetContent("")
	return &ConversationView{
		conversation:        []domain.ConversationEntry{},
		Viewport:            vp,
		width:               80,
		height:              20,
		expandedToolResults: make(map[int]bool),
		allToolsExpanded:    false,
		lineFormatter:       shared.NewConversationLineFormatter(80, nil),
		plainTextLines:      []string{},
		themeService:        themeService,
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

	statusColor := cv.getStatusColor()
	successColor := cv.getSuccessColor()
	dimColor := cv.getDimColor()
	headerColor := cv.getHeaderColor()

	headerLine := statusColor + "✨ Inference Gateway CLI" + colors.Reset
	readyLine := successColor + "🚀 Ready to chat!" + colors.Reset
	workingLine := dimColor + "📂 Working in: " + colors.Reset + headerColor + wd + colors.Reset

	configLine := cv.buildConfigLine()

	content := headerLine + "\n\n" + readyLine + "\n\n" + workingLine + "\n\n" + configLine

	style := styles.NewCommonStyles().Border.
		Border(styles.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(cv.getAccentColorLipgloss())).
		Padding(1, 1)

	return style.Render(content)
}

func (cv *ConversationView) renderEntryWithIndex(entry domain.ConversationEntry, index int) string {
	var color, role string

	switch string(entry.Message.Role) {
	case "user":
		color = cv.getUserColor()
		role = "> You"
	case "assistant":
		color = cv.getAssistantColor()
		if entry.Model != "" {
			role = fmt.Sprintf("⏺ %s", entry.Model)
		} else {
			role = "⏺ Assistant"
		}
		// Check if assistant message has tool calls
		if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
			return cv.renderAssistantWithToolCalls(entry, index, color, role)
		}
	case "system":
		color = cv.getDimColor()
		role = "⚙️ System"
	case "tool":
		if entry.ToolExecution != nil && !entry.ToolExecution.Success {
			color = cv.getErrorColor()
			role = "🔧 Tool"
		} else if entry.ToolExecution != nil && entry.ToolExecution.Success {
			color = cv.getSuccessColor()
			role = "🔧 Tool"
		} else {
			color = cv.getAccentColor()
			role = "🔧 Tool"
		}
		return cv.renderToolEntry(entry, index, color, role)
	default:
		color = cv.getDimColor()
		role = string(entry.Message.Role)
	}

	content := entry.Message.Content
	wrappedContent := shared.FormatResponsiveMessage(content, cv.width)
	message := fmt.Sprintf("%s%s:%s %s", color, role, colors.Reset, wrappedContent)

	return message + "\n"
}

func (cv *ConversationView) renderAssistantWithToolCalls(entry domain.ConversationEntry, _ int, color, role string) string {
	var result strings.Builder

	if entry.Message.Content != "" {
		wrappedContent := shared.FormatResponsiveMessage(entry.Message.Content, cv.width)
		result.WriteString(fmt.Sprintf("%s%s:%s %s\n", color, role, colors.Reset, wrappedContent))
	} else {
		result.WriteString(fmt.Sprintf("%s%s:%s\n", color, role, colors.Reset))
	}

	if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 { // nolint:nestif
		toolCallsColor := cv.getAccentColor()
		result.WriteString(fmt.Sprintf("\n%s🔧 Tool Calls:%s\n", toolCallsColor, colors.Reset))

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
				result.WriteString(fmt.Sprintf("  • %s%s%s: %s\n",
					toolCallsColor, toolName, colors.Reset, argsDisplay))
			} else {
				result.WriteString(fmt.Sprintf("  • %s%s%s\n",
					toolCallsColor, toolName, colors.Reset))
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
	message := fmt.Sprintf("%s%s:%s %s", color, role, colors.Reset, content)
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
			helpText = "\n💡 Press Ctrl+R to collapse all tool calls"
		}

		return content + helpText
	}
	wrappedContent := shared.FormatResponsiveMessage(entry.Message.Content, cv.width)
	return wrappedContent + "\n\n💡 Press Ctrl+R to collapse all tool calls"
}

func (cv *ConversationView) formatCompactContent(entry domain.ConversationEntry) string {
	if entry.ToolExecution != nil {
		content := cv.toolFormatter.FormatToolResultForUI(entry.ToolExecution, cv.width)
		return content + "\n💡 Press Ctrl+R to expand all tool calls"
	}
	content := cv.formatToolContentCompact(entry.Message.Content)
	wrappedContent := shared.FormatResponsiveMessage(content, cv.width)
	return wrappedContent + "\n💡 Press Ctrl+R to expand all tool calls"
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

	dimColor := cv.getDimColor()
	accentColor := cv.getAccentColor()

	return dimColor + "⚙  Config: " + colors.Reset + accentColor + displayPath + colors.Reset + dimColor + " (" + configType + ")" + colors.Reset
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

	switch msg := msg.(type) {
	case domain.UpdateHistoryEvent:
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
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetUserColor()
	}
	return colors.UserColor.ANSI
}

func (cv *ConversationView) getAssistantColor() string {
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetAssistantColor()
	}
	return colors.AssistantColor.ANSI
}

func (cv *ConversationView) getErrorColor() string {
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetErrorColor()
	}
	return colors.ErrorColor.ANSI
}

func (cv *ConversationView) getStatusColor() string {
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetStatusColor()
	}
	return colors.StatusColor.ANSI
}

func (cv *ConversationView) getSuccessColor() string {
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetStatusColor()
	}
	return colors.SuccessColor.ANSI
}

func (cv *ConversationView) getAccentColor() string {
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetAccentColor()
	}
	return colors.AccentColor.ANSI
}

func (cv *ConversationView) getDimColor() string {
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetDimColor()
	}
	return colors.DimColor.ANSI
}

func (cv *ConversationView) getHeaderColor() string {
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetAccentColor()
	}
	return colors.HeaderColor.ANSI
}

func (cv *ConversationView) getAccentColorLipgloss() string {
	if cv.themeService != nil {
		return cv.themeService.GetCurrentTheme().GetAccentColor()
	}
	return colors.AccentColor.Lipgloss
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
				Content: content,
			},
			Time: time.Now(),
		}
		cv.conversation = append(cv.conversation, streamingEntry)
	} else {
		lastIdx := len(cv.conversation) - 1
		cv.conversation[lastIdx].Message.Content += content
	}

	cv.updatePlainTextLines()
	cv.updateViewportContent()
}
