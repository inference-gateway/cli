package components

import (
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// ApprovalComponent handles rendering of tool approval requests
type ApprovalComponent struct {
	width        int
	height       int
	theme        shared.Theme
	diffRenderer *DiffRenderer
}

// NewApprovalComponent creates a new approval component
func NewApprovalComponent(theme shared.Theme) *ApprovalComponent {
	return &ApprovalComponent{
		theme:        theme,
		diffRenderer: NewDiffRenderer(theme),
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

	var b strings.Builder

	// Header
	b.WriteString("🔧 Tool Approval Required\n")
	b.WriteString(strings.Repeat("─", a.width))
	b.WriteString("\n")

	b.WriteString(fmt.Sprintf("Tool: %s\n", currentTool.Name))

	// Show detailed arguments and previews for different tools
	switch currentTool.Name {
	case "Edit":
		b.WriteString(a.diffRenderer.RenderEditToolArguments(currentTool.Arguments))
	case "MultiEdit":
		b.WriteString(a.diffRenderer.RenderMultiEditToolArguments(currentTool.Arguments))
	default:
		b.WriteString("Arguments:\n")
		if currentTool.Arguments != nil {
			for key, value := range currentTool.Arguments {
				b.WriteString(fmt.Sprintf("  • %s: %v\n", key, value))
			}
		}
	}

	b.WriteString("\n")
	b.WriteString("⚠️  This tool will execute on your system. Please review carefully.\n\n")

	b.WriteString("Please select an action:\n\n")

	options := []string{
		"✅ Approve and execute",
		"❌ Deny and cancel",
	}

	for i, option := range options {
		if i == selectedIndex {
			b.WriteString(fmt.Sprintf("%s▶ %s%s\n", a.theme.GetAccentColor(), option, "\033[0m"))
		} else {
			b.WriteString(fmt.Sprintf("%s  %s%s\n", a.theme.GetDimColor(), option, "\033[0m"))
		}
	}

	b.WriteString("\n")

	helpText := "Use ↑↓ arrow keys to navigate, SPACE to select, ESC to cancel"
	b.WriteString(a.theme.GetDimColor() + helpText + "\033[0m")

	return b.String()
}
