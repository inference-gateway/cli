package components

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
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
}

func NewConversationView() *ConversationView {
	vp := viewport.New(80, 20)
	vp.SetContent("")
	return &ConversationView{
		conversation:        []domain.ConversationEntry{},
		Viewport:            vp,
		width:               80,
		height:              20,
		expandedToolResults: make(map[int]bool),
		allToolsExpanded:    false,
	}
}

// SetToolFormatter sets the tool formatter for this conversation view
func (cv *ConversationView) SetToolFormatter(formatter domain.ToolFormatter) {
	cv.toolFormatter = formatter
}

func (cv *ConversationView) SetConversation(conversation []domain.ConversationEntry) {
	cv.conversation = conversation
	cv.updateViewportContent()
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

func (cv *ConversationView) SetWidth(width int) {
	cv.width = width
	cv.Viewport.Width = width
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
	return cv.Viewport.View()
}

func (cv *ConversationView) updateViewportContent() {
	var b strings.Builder

	displayIndex := 0
	for i, entry := range cv.conversation {
		if entry.IsSystemReminder {
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

	headerLine := shared.StatusColor.ANSI + "✨ Inference Gateway CLI" + shared.Reset()
	readyLine := shared.SuccessColor.ANSI + "🚀 Ready to chat!" + shared.Reset()
	workingLine := shared.DimColor.ANSI + "📂 Working in: " + shared.Reset() + shared.HeaderColor.ANSI + wd + shared.Reset()
	helpLine := shared.DimColor.ANSI + "Type your message or try commands like /help" + shared.Reset()

	content := headerLine + "\n" + readyLine + "\n" + workingLine + "\n" + helpLine

	style := shared.NewCommonStyles().Border.
		Border(shared.RoundedBorder(), true).
		BorderForeground(shared.AccentColor.GetLipglossColor()).
		Padding(0, 1)

	return style.Render(content)
}

func (cv *ConversationView) renderEntryWithIndex(entry domain.ConversationEntry, index int) string {
	var color, role string

	switch string(entry.Message.Role) {
	case "user":
		color = shared.UserColor.ANSI
		role = "> You"
	case "assistant":
		color = shared.AssistantColor.ANSI
		if entry.Model != "" {
			role = fmt.Sprintf("⏺ %s", entry.Model)
		} else {
			role = "⏺ Assistant"
		}
	case "system":
		color = shared.DimColor.ANSI
		role = "⚙️ System"
	case "tool":
		// Determine tool color based on success/failure
		if entry.ToolExecution != nil && !entry.ToolExecution.Success {
			color = shared.ErrorColor.ANSI
			role = "🔧 Tool"
		} else if entry.ToolExecution != nil && entry.ToolExecution.Success {
			color = shared.SuccessColor.ANSI
			role = "🔧 Tool"
		} else {
			color = shared.AccentColor.ANSI
			role = "🔧 Tool"
		}
		return cv.renderToolEntry(entry, index, color, role)
	default:
		color = shared.DimColor.ANSI
		role = string(entry.Message.Role)
	}

	content := entry.Message.Content
	wrappedContent := shared.FormatResponsiveMessage(content, cv.width)
	message := fmt.Sprintf("%s%s:%s %s", color, role, shared.Reset(), wrappedContent)

	return message + "\n"
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
	message := fmt.Sprintf("%s%s:%s %s", color, role, shared.Reset(), content)
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
		// Fallback to original truncation logic
		lines := strings.Split(content, "\n")
		if len(lines) <= 3 {
			return content
		}
		return strings.Join(lines[:3], "\n") + "\n... (truncated)"
	}

	// Parse tool calls from content and format them properly
	lines := strings.Split(content, "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if toolCall := cv.parseToolCallFromLine(trimmed); toolCall != nil {
			// Use the proper formatter for collapsed display
			formattedCall := cv.toolFormatter.FormatToolCall(toolCall.Name, toolCall.Args)
			result = append(result, "Tool: "+formattedCall)
		} else {
			result = append(result, line)
		}
	}

	// Apply original truncation logic if needed
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
	case shared.UpdateHistoryMsg:
		cv.SetConversation(msg.History)
		return cv, cmd
	case shared.ScrollRequestMsg:
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

func (cv *ConversationView) handleScrollRequest(msg shared.ScrollRequestMsg) (tea.Model, tea.Cmd) {
	switch msg.Direction {
	case shared.ScrollUp:
		for i := 0; i < msg.Amount; i++ {
			cv.Viewport.ScrollUp(1)
		}
	case shared.ScrollDown:
		for i := 0; i < msg.Amount; i++ {
			cv.Viewport.ScrollDown(1)
		}
	case shared.ScrollToTop:
		cv.Viewport.GotoTop()
	case shared.ScrollToBottom:
		cv.Viewport.GotoBottom()
	}
	return cv, nil
}
