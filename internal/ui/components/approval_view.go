package components

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/muesli/reflow/wordwrap"
)

// ApprovalComponent handles rendering of tool approval requests
type ApprovalComponent struct {
	width        int
	height       int
	theme        shared.Theme
	diffRenderer *DiffRenderer
	styles       *approvalStyles
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
			Foreground(lipgloss.Color("39")).
			Bold(true),
		border: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")),
		toolName: lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true),
		argumentKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color("36")).
			Bold(true),
		argumentValue: lipgloss.NewStyle().
			Foreground(lipgloss.Color("253")),
		warning: lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true),
		prompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Bold(true),
		selectedOption: lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true).
			Background(lipgloss.Color("235")).
			Padding(0, 1),
		unselectedOption: lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Padding(0, 1),
		helpText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true),
		container: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
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

// Render renders the approval component for a given tool execution
func (a *ApprovalComponent) Render(toolExecution *domain.ToolExecutionSession, selectedIndex int) string {
	if toolExecution == nil || !toolExecution.RequiresApproval {
		return ""
	}

	currentTool := toolExecution.CurrentTool
	if currentTool == nil {
		return ""
	}

	var content strings.Builder

	titleText := a.styles.title.Render("🔧 Tool Approval Required")
	content.WriteString(titleText)
	content.WriteString("\n")

	toolSection := fmt.Sprintf("%s %s",
		a.styles.prompt.Render("Tool:"),
		a.styles.toolName.Render(currentTool.Name))
	content.WriteString(toolSection)
	content.WriteString("\n")

	switch currentTool.Name {
	case "Edit":
		content.WriteString(a.diffRenderer.RenderEditToolArguments(currentTool.Arguments))
	case "MultiEdit":
		content.WriteString(a.diffRenderer.RenderMultiEditToolArguments(currentTool.Arguments))
	default:
		if len(currentTool.Arguments) > 0 {
			content.WriteString(a.styles.prompt.Render("Arguments:"))
			content.WriteString("\n")

			keys := make([]string, 0, len(currentTool.Arguments))
			for key := range currentTool.Arguments {
				keys = append(keys, key)
			}
			sort.Strings(keys)

			argBoxWidth := a.width - 14
			if argBoxWidth < 30 {
				argBoxWidth = 30
			}
			argsBox := lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("238")).
				Padding(0, 1).
				Width(argBoxWidth)

			var argsContent strings.Builder
			for i, key := range keys {
				value := currentTool.Arguments[key]
				keyStr := a.styles.argumentKey.Render(key + ":")

				valueStr := fmt.Sprintf("%v", value)
				if len(valueStr) > 60 {
					valueStr = valueStr[:57] + "..."
				}
				valueRendered := a.styles.argumentValue.Render(valueStr)

				argsContent.WriteString(fmt.Sprintf("%s %s", keyStr, valueRendered))
				if i < len(keys)-1 {
					argsContent.WriteString("\n")
				}
			}
			content.WriteString(argsBox.Render(argsContent.String()))
		}
	}

	content.WriteString("\n")

	warningTextOnly := "This tool will execute on your system. Please review carefully."

	textWidth := a.width - 12
	if textWidth < 40 {
		textWidth = 40
	}

	wrappedText := wordwrap.String(warningTextOnly, textWidth)
	lines := strings.Split(wrappedText, "\n")

	for _, line := range lines {
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

	helpMsg := a.styles.helpText.Render("↑↓ Navigate  •  SPACE Select  •  ESC Cancel")
	content.WriteString(helpMsg)

	contentStr := content.String()

	containerWidth := a.width - 6
	if containerWidth < 40 {
		containerWidth = 40
	}

	contentLines := strings.Split(contentStr, "\n")
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

	finalContent := a.styles.container.
		Width(containerWidth).
		MaxHeight(15).
		Render(wrappedContent.String())

	return finalContent
}
