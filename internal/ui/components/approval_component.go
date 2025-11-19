package components

import (
	"encoding/json"
	"fmt"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// ToolFormatterService interface for formatting tool arguments
type ToolFormatterService interface {
	FormatToolArgumentsForApproval(toolName string, args map[string]any) string
}

// ApprovalComponent renders the tool approval modal
type ApprovalComponent struct {
	width         int
	height        int
	toolFormatter ToolFormatterService
	styleProvider *styles.Provider
}

// NewApprovalComponent creates a new approval component
func NewApprovalComponent(styleProvider *styles.Provider) *ApprovalComponent {
	return &ApprovalComponent{
		styleProvider: styleProvider,
	}
}

// SetDimensions updates the component dimensions
func (c *ApprovalComponent) SetDimensions(width, height int) {
	c.width = width
	c.height = height
}

// SetToolFormatter sets the tool formatter service
func (c *ApprovalComponent) SetToolFormatter(formatter ToolFormatterService) {
	c.toolFormatter = formatter
}

// Render renders the approval modal
func (c *ApprovalComponent) Render(approvalState *domain.ApprovalUIState, theme domain.Theme) string {
	if approvalState == nil || approvalState.PendingToolCall == nil {
		return ""
	}

	toolCall := approvalState.PendingToolCall

	modalWidth := min(c.width-4, 80)

	title := c.styleProvider.RenderStyledText("🔒 Tool Approval Required", styles.StyleOptions{
		Foreground:   c.styleProvider.GetThemeColor("accent"),
		Bold:         true,
		MarginBottom: 1,
	})

	toolNameStyled := c.styleProvider.RenderWithColorAndBold(toolCall.Function.Name, c.styleProvider.GetThemeColor("accent"))
	toolName := fmt.Sprintf("Tool: %s", toolNameStyled)

	args := c.formatArguments(toolCall.Function.Name, toolCall.Function.Arguments, modalWidth-4)

	options := c.renderOptions(approvalState.SelectedIndex)

	helpText := c.styleProvider.RenderStyledText(
		"←/→: Navigate • Enter/y: Approve • n: Reject • a: Auto-Accept • Esc: Cancel",
		styles.StyleOptions{
			Foreground: c.styleProvider.GetThemeColor("dim"),
			Italic:     true,
			MarginTop:  1,
		},
	)

	content := c.styleProvider.JoinVertical(
		title,
		toolName,
		args,
		"",
		c.styleProvider.PlaceCenterTop(modalWidth, c.styleProvider.GetHeight(options), options),
		helpText,
	)

	modal := c.styleProvider.RenderModal(content, modalWidth)
	return c.styleProvider.PlaceCenter(c.width, c.height, modal)
}

// formatArguments formats tool arguments for display
func (c *ApprovalComponent) formatArguments(toolName, argsJSON string, maxWidth int) string {
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

	if c.toolFormatter != nil {
		formatted := c.toolFormatter.FormatToolArgumentsForApproval(toolName, args)
		if formatted != "" {
			return "\n" + formatted
		}
	}

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

	argsText := strings.Join(argLines, "\n")
	return c.styleProvider.RenderStyledText(argsText, styles.StyleOptions{
		Foreground: c.styleProvider.GetThemeColor("dim"),
		Width:      maxWidth,
	})
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

	if len(str) > maxLen {
		if maxLen > 3 {
			str = str[:maxLen-3] + "..."
		} else {
			str = str[:maxLen]
		}
	}

	str = strings.ReplaceAll(str, "\n", " ")
	str = strings.ReplaceAll(str, "\r", "")

	return str
}

// renderOptions renders the Approve/Reject/Auto-Accept options
func (c *ApprovalComponent) renderOptions(selectedIndex int) string {
	isApproveSelected := selectedIndex == int(domain.ApprovalApprove)
	isRejectSelected := selectedIndex == int(domain.ApprovalReject)
	isAutoAcceptSelected := selectedIndex == int(domain.ApprovalAutoAccept)

	var approveIcon, rejectIcon, autoAcceptIcon string

	if isApproveSelected {
		approveIcon = "✓ "
	}
	if isRejectSelected {
		rejectIcon = "✗ "
	}
	if isAutoAcceptSelected {
		autoAcceptIcon = "⚡ "
	}

	approveText := approveIcon + "Approve"
	rejectText := rejectIcon + "Reject"
	autoAcceptText := autoAcceptIcon + "Auto-Accept"

	approveButton := c.styleProvider.RenderApprovalButton(approveText, isApproveSelected, true)
	rejectButton := c.styleProvider.RenderApprovalButton(rejectText, isRejectSelected, false)
	autoAcceptButton := c.styleProvider.RenderApprovalButton(autoAcceptText, isAutoAcceptSelected, true)

	return c.styleProvider.JoinHorizontal(approveButton, "  ", rejectButton, "  ", autoAcceptButton)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
