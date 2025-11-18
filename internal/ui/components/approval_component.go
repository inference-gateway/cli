package components

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// ApprovalComponent renders the tool approval modal
type ApprovalComponent struct {
	width  int
	height int
}

// NewApprovalComponent creates a new approval component
func NewApprovalComponent() *ApprovalComponent {
	return &ApprovalComponent{}
}

// SetDimensions updates the component dimensions
func (c *ApprovalComponent) SetDimensions(width, height int) {
	c.width = width
	c.height = height
}

// Render renders the approval modal
func (c *ApprovalComponent) Render(approvalState *domain.ApprovalUIState, theme domain.Theme) string {
	if approvalState == nil || approvalState.PendingToolCall == nil {
		return ""
	}

	toolCall := approvalState.PendingToolCall

	borderColor := theme.GetBorderColor()
	accentColor := theme.GetAccentColor()
	errorColor := theme.GetErrorColor()
	dimColor := theme.GetDimColor()

	modalWidth := min(c.width-4, 80)
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(1, 2).
		Width(modalWidth)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(accentColor)).
		MarginBottom(1)

	title := titleStyle.Render("🔒 Tool Approval Required")

	toolNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(accentColor))

	toolName := fmt.Sprintf("Tool: %s", toolNameStyle.Render(toolCall.Function.Name))

	args := c.formatArguments(toolCall.Function.Arguments, modalWidth-4, dimColor)

	options := c.renderOptions(approvalState.SelectedIndex, accentColor, errorColor)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(dimColor)).
		Italic(true).
		MarginTop(1)

	helpText := helpStyle.Render("←/→: Navigate  •  Enter/y: Approve  •  n/Esc: Reject")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		toolName,
		args,
		"",
		lipgloss.Place(modalWidth, lipgloss.Height(options), lipgloss.Center, lipgloss.Top, options),
		helpText,
	)

	return lipgloss.Place(c.width, c.height, lipgloss.Center, lipgloss.Center, modalStyle.Render(content))
}

// formatArguments formats tool arguments for display
func (c *ApprovalComponent) formatArguments(argsJSON string, maxWidth int, dimColor string) string {
	if argsJSON == "" {
		return ""
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}

	if len(args) == 0 {
		return ""
	}

	argStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(dimColor)).
		Width(maxWidth)

	var argLines []string
	argLines = append(argLines, "\nArguments:")

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}

	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	for _, key := range keys {
		valueStr := c.formatValue(args[key], maxWidth-len(key)-4)
		line := fmt.Sprintf("  %s: %s", key, valueStr)
		argLines = append(argLines, line)
	}

	return argStyle.Render(strings.Join(argLines, "\n"))
}

// formatValue formats a single argument value, truncating if necessary
func (c *ApprovalComponent) formatValue(value any, maxLen int) string {
	var str string

	switch v := value.(type) {
	case string:
		str = v
	case map[string]any, []any:
		jsonBytes, _ := json.Marshal(v)
		str = string(jsonBytes)
	default:
		str = fmt.Sprintf("%v", v)
	}

	// Truncate long values
	if len(str) > maxLen {
		if maxLen > 3 {
			str = str[:maxLen-3] + "..."
		} else {
			str = str[:maxLen]
		}
	}

	// Replace newlines with spaces for compact display
	str = strings.ReplaceAll(str, "\n", " ")
	str = strings.ReplaceAll(str, "\r", "")

	return str
}

// renderOptions renders the Approve/Reject options
func (c *ApprovalComponent) renderOptions(selectedIndex int, accentColor, errorColor string) string {
	approveStyle := lipgloss.NewStyle().
		Padding(0, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accentColor))

	rejectStyle := lipgloss.NewStyle().
		Padding(0, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(errorColor))

	selectedApproveStyle := approveStyle.
		Background(lipgloss.Color(accentColor)).
		Foreground(lipgloss.Color("#000000")).
		Bold(true)

	selectedRejectStyle := rejectStyle.
		Background(lipgloss.Color(errorColor)).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true)

	var approveText, rejectText string

	if selectedIndex == int(domain.ApprovalApprove) {
		approveText = selectedApproveStyle.Render("✓ Approve")
	} else {
		approveText = approveStyle.Render("  Approve")
	}

	if selectedIndex == int(domain.ApprovalReject) {
		rejectText = selectedRejectStyle.Render("✗ Reject")
	} else {
		rejectText = rejectStyle.Render("  Reject")
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		approveText,
		"  ",
		rejectText,
	)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
