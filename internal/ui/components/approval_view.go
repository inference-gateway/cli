package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// ApprovalComponent handles rendering of tool approval requests
type ApprovalComponent struct {
	width  int
	height int
	theme  shared.Theme
}

// NewApprovalComponent creates a new approval component
func NewApprovalComponent(theme shared.Theme) *ApprovalComponent {
	return &ApprovalComponent{
		theme: theme,
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

	// Tool info - compact version
	b.WriteString(fmt.Sprintf("Tool: %s", currentTool.Name))

	// Show key arguments inline for common tools
	switch currentTool.Name {
	case "Read":
		if filePath, ok := currentTool.Arguments["file_path"].(string); ok {
			b.WriteString(fmt.Sprintf(" → %s", filePath))
		}
	case "Edit":
		if filePath, ok := currentTool.Arguments["file_path"].(string); ok {
			b.WriteString(fmt.Sprintf(" → %s", filePath))
		}
	case "Bash":
		if command, ok := currentTool.Arguments["command"].(string); ok {
			if len(command) > 50 {
				command = command[:47] + "..."
			}
			b.WriteString(fmt.Sprintf(" → %s", command))
		}
	case "Write":
		if filePath, ok := currentTool.Arguments["file_path"].(string); ok {
			b.WriteString(fmt.Sprintf(" → %s", filePath))
		}
	}
	b.WriteString("\n\n")

	// Options stacked vertically
	options := []string{"✅ Approve (Enter)", "❌ Deny (Esc)"}

	for i, option := range options {
		if i == selectedIndex {
			// Highlight selected option with arrow indicator
			highlightStyle := lipgloss.NewStyle().
				Background(lipgloss.Color(a.theme.GetAccentColor())).
				Foreground(lipgloss.Color("#ffffff")).
				Padding(0, 1)
			b.WriteString("▶ " + highlightStyle.Render(option))
		} else {
			b.WriteString("  " + option)
		}
		b.WriteString("\n")
	}

	// Help text
	helpText := "⚠️  This tool will execute on your system. Press Enter to approve, Esc to deny."
	b.WriteString(a.theme.GetDimColor() + helpText + "\033[0m")

	return b.String()
}
