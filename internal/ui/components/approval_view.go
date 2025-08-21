package components

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/muesli/reflow/wordwrap"
)

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ApprovalComponent handles rendering of tool approval requests
type ApprovalComponent struct {
	width        int
	height       int
	theme        shared.Theme
	diffRenderer *DiffRenderer
	styles       *approvalStyles
	scrollOffset int
}

type approvalStyles struct {
	title            lipgloss.Style
	border           lipgloss.Style
	toolName         lipgloss.Style
	argumentKey      lipgloss.Style
	argumentValue    lipgloss.Style
	warning          lipgloss.Style
	prompt           lipgloss.Style
	selectedOption   lipgloss.Style
	unselectedOption lipgloss.Style
	helpText         lipgloss.Style
	container        lipgloss.Style
}

// NewApprovalComponent creates a new approval component
func NewApprovalComponent(theme shared.Theme) *ApprovalComponent {
	styles := &approvalStyles{
		title: lipgloss.NewStyle().
			Foreground(shared.HeaderColor.GetLipglossColor()).
			Bold(true),
		border: lipgloss.NewStyle().
			Foreground(shared.BorderColor.GetLipglossColor()),
		toolName: lipgloss.NewStyle().
			Foreground(shared.AccentColor.GetLipglossColor()).
			Bold(true),
		argumentKey: lipgloss.NewStyle().
			Foreground(shared.AccentColor.GetLipglossColor()).
			Bold(true),
		argumentValue: lipgloss.NewStyle().
			Foreground(shared.AssistantColor.GetLipglossColor()),
		warning: lipgloss.NewStyle().
			Foreground(shared.WarningColor.GetLipglossColor()).
			Bold(true),
		prompt: lipgloss.NewStyle().
			Foreground(shared.AssistantColor.GetLipglossColor()).
			Bold(true),
		selectedOption: lipgloss.NewStyle().
			Foreground(shared.SuccessColor.GetLipglossColor()).
			Bold(true).
			Background(lipgloss.Color("#1f2335")).
			Padding(0, 1),
		unselectedOption: lipgloss.NewStyle().
			Foreground(shared.DimColor.GetLipglossColor()).
			Padding(0, 1),
		helpText: lipgloss.NewStyle().
			Foreground(shared.DimColor.GetLipglossColor()).
			Italic(true),
		container: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(shared.BorderColor.GetLipglossColor()).
			Padding(0, 1),
	}

	return &ApprovalComponent{
		theme:        theme,
		diffRenderer: NewDiffRenderer(theme),
		styles:       styles,
	}
}

// SetWidth sets the component width
func (a *ApprovalComponent) SetWidth(width int) {
	a.width = width
}

// SetHeight sets the component height
func (a *ApprovalComponent) SetHeight(height int) {
	a.height = height
}

// Init implements the Bubble Tea Model interface
func (a *ApprovalComponent) Init() tea.Cmd {
	return nil
}

// Update handles Bubble Tea messages including scroll requests
func (a *ApprovalComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case shared.ScrollRequestMsg:
		if msg.ComponentID == "approval" {
			return a.handleScrollRequest(msg)
		}
	}
	return a, nil
}

// View implements the Bubble Tea Model interface
func (a *ApprovalComponent) View() string {
	return ""
}

// handleScrollRequest processes scroll requests for the approval component
func (a *ApprovalComponent) handleScrollRequest(msg shared.ScrollRequestMsg) (tea.Model, tea.Cmd) {
	switch msg.Direction {
	case shared.ScrollUp:
		for i := 0; i < msg.Amount; i++ {
			if a.scrollOffset > 0 {
				a.scrollOffset--
			}
		}
	case shared.ScrollDown:
		for i := 0; i < msg.Amount; i++ {
			a.scrollOffset++
		}
	case shared.ScrollToTop:
		a.scrollOffset = 0
	case shared.ScrollToBottom:
		a.scrollOffset = 1000
	}
	return a, nil
}

// Render renders the approval component for a given tool execution
func (a *ApprovalComponent) Render(toolExecution *domain.ToolExecutionSession, selectedIndex int) string {
	if toolExecution == nil || !toolExecution.RequiresApproval {
		return ""
	}

	currentTool := toolExecution.CurrentTool
	if currentTool == nil {
		return ""
	}

	headerContent := a.renderHeader(currentTool)
	toolContent := a.renderToolContent(currentTool)
	footerContent := a.renderFooter(selectedIndex)

	return a.assembleContent(headerContent, toolContent, footerContent)
}

// renderHeader renders the title and tool name section
func (a *ApprovalComponent) renderHeader(currentTool *domain.ToolCall) string {
	var content strings.Builder

	titleText := a.styles.title.Render("🔧 Tool Approval Required")
	content.WriteString(titleText)
	content.WriteString("\n")

	toolSection := fmt.Sprintf("%s %s",
		a.styles.prompt.Render("Tool:"),
		a.styles.toolName.Render(currentTool.Name))
	content.WriteString(toolSection)
	content.WriteString("\n")

	return content.String()
}

// renderToolContent renders the tool-specific content based on tool type
func (a *ApprovalComponent) renderToolContent(currentTool *domain.ToolCall) string {
	switch currentTool.Name {
	case "Edit":
		return a.diffRenderer.RenderEditToolArguments(currentTool.Arguments)
	case "MultiEdit":
		return a.diffRenderer.RenderMultiEditToolArguments(currentTool.Arguments)
	default:
		return a.renderDefaultArguments(currentTool.Arguments)
	}
}

// renderDefaultArguments renders tool arguments for non-edit tools
func (a *ApprovalComponent) renderDefaultArguments(arguments map[string]interface{}) string {
	if len(arguments) == 0 {
		return ""
	}

	var content strings.Builder
	content.WriteString(a.styles.prompt.Render("Arguments:"))
	content.WriteString("\n")

	keys := make([]string, 0, len(arguments))
	for key := range arguments {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	argBoxWidth := max(a.width-14, 30)
	argsBox := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(shared.BorderColor.GetLipglossColor()).
		Padding(0, 1).
		Width(argBoxWidth)

	var argsContent strings.Builder
	for i, key := range keys {
		value := arguments[key]
		keyStr := a.styles.argumentKey.Render(key + ":")

		valueStr := fmt.Sprintf("%v", value)
		// Don't truncate content in the approval view - let scrolling handle large content
		valueRendered := a.styles.argumentValue.Render(valueStr)

		argsContent.WriteString(fmt.Sprintf("%s %s", keyStr, valueRendered))
		if i < len(keys)-1 {
			argsContent.WriteString("\n")
		}
	}
	content.WriteString(argsBox.Render(argsContent.String()))

	return content.String()
}

// renderFooter renders the warning, options, and help text
func (a *ApprovalComponent) renderFooter(selectedIndex int) string {
	var content strings.Builder

	warningTextOnly := "This tool will execute on your system. Please review carefully."
	textWidth := max(a.width-12, 40)
	wrappedText := wordwrap.String(warningTextOnly, textWidth)
	for line := range strings.SplitSeq(wrappedText, "\n") {
		warningMsg := a.styles.warning.Render(line)
		content.WriteString(warningMsg)
		content.WriteString("\n")
	}

	promptMsg := a.styles.prompt.Render("Select an action:")
	content.WriteString(promptMsg)
	content.WriteString("\n")

	options := []struct {
		icon string
		text string
	}{
		{"✅", "Approve and execute"},
		{"❌", "Deny and cancel"},
	}

	for i, opt := range options {
		optionText := fmt.Sprintf("%s %s", opt.icon, opt.text)
		if i == selectedIndex {
			content.WriteString("  ")
			content.WriteString(a.styles.selectedOption.Render("▶ " + optionText))
		} else {
			content.WriteString("  ")
			content.WriteString(a.styles.unselectedOption.Render("  " + optionText))
		}
		if i < len(options)-1 {
			content.WriteString("\n")
		}
	}

	content.WriteString("\n")

	helpMsg := a.styles.helpText.Render("↑↓ Navigate  •  SHIFT+↑↓ Scroll  •  SPACE Select  •  ESC Cancel")
	content.WriteString(helpMsg)

	return content.String()
}

// assembleContent combines header, tool content, and footer with height management
func (a *ApprovalComponent) assembleContent(headerStr, toolStr, footerStr string) string {
	containerWidth := max(a.width-6, 40)

	headerLines := len(strings.Split(headerStr, "\n"))
	footerLines := len(strings.Split(footerStr, "\n"))

	fixedLines := headerLines + footerLines + 1
	availableHeight := max(int(float64(a.height)*0.8), 10+fixedLines)
	maxToolHeight := max(availableHeight-fixedLines, 10)

	var finalContent strings.Builder
	finalContent.WriteString(headerStr)

	if toolStr != "" {
		finalContent.WriteString("\n")
		a.renderToolContentWithScrolling(&finalContent, toolStr, maxToolHeight)
	}

	finalContent.WriteString("\n")
	finalContent.WriteString(footerStr)

	contentLines := strings.Split(finalContent.String(), "\n")
	var wrappedContent strings.Builder
	for i, line := range contentLines {
		if len(line) > containerWidth-4 {
			wrapped := wordwrap.String(line, containerWidth-4)
			wrappedContent.WriteString(wrapped)
		} else {
			wrappedContent.WriteString(line)
		}
		if i < len(contentLines)-1 {
			wrappedContent.WriteString("\n")
		}
	}

	return a.styles.container.
		Width(containerWidth).
		Render(wrappedContent.String())
}

// renderToolContentWithScrolling renders the tool content with scrolling support
func (a *ApprovalComponent) renderToolContentWithScrolling(finalContent *strings.Builder, toolStr string, maxToolHeight int) {
	toolLines := strings.Split(toolStr, "\n")
	totalLines := len(toolLines)

	if totalLines <= maxToolHeight {
		finalContent.WriteString(toolStr)
		return
	}

	startLine := a.scrollOffset
	endLine := min(startLine+maxToolHeight-1, totalLines)

	if startLine >= totalLines {
		startLine = max(0, totalLines-maxToolHeight+1)
		a.scrollOffset = startLine
	}

	if endLine > totalLines {
		endLine = totalLines
	}

	if startLine >= endLine {
		finalContent.WriteString(a.styles.helpText.Render("... (content scrolled) ..."))
		return
	}

	visibleLines := toolLines[startLine:endLine]
	finalContent.WriteString(strings.Join(visibleLines, "\n"))
	finalContent.WriteString("\n")

	scrollInfo := fmt.Sprintf("... (line %d-%d of %d, shift+↑↓ to scroll) ...",
		startLine+1, endLine, totalLines)
	finalContent.WriteString(a.styles.helpText.Render(scrollInfo))
}
