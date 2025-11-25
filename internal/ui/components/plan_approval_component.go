package components

import (
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// PlanApprovalComponent renders the plan approval modal
type PlanApprovalComponent struct {
	width         int
	height        int
	styleProvider *styles.Provider
}

// NewPlanApprovalComponent creates a new plan approval component
func NewPlanApprovalComponent(styleProvider *styles.Provider) *PlanApprovalComponent {
	return &PlanApprovalComponent{
		styleProvider: styleProvider,
	}
}

// SetDimensions updates the component dimensions
func (c *PlanApprovalComponent) SetDimensions(width, height int) {
	c.width = width
	c.height = height
}

// Render renders the plan approval modal
func (c *PlanApprovalComponent) Render(planApprovalState *domain.PlanApprovalUIState, theme domain.Theme) string {
	if planApprovalState == nil {
		return ""
	}

	modalWidth := min(c.width-4, 100)

	title := c.styleProvider.RenderStyledText("📋 Plan Approval Required", styles.StyleOptions{
		Foreground:   c.styleProvider.GetThemeColor("accent"),
		Bold:         true,
		MarginBottom: 1,
	})

	planHeader := c.styleProvider.RenderStyledText("The agent has completed planning. Review the plan below:", styles.StyleOptions{
		Foreground:   c.styleProvider.GetThemeColor("dim"),
		MarginBottom: 1,
	})

	planContent := c.formatPlanContent(planApprovalState.PlanContent, modalWidth-4)

	options := c.renderOptions(planApprovalState.SelectedIndex)

	helpText := c.styleProvider.RenderStyledText(
		"←/→: Navigate • Enter/y: Accept • n: Reject • a: Auto-Approve • Esc: Reject",
		styles.StyleOptions{
			Foreground: c.styleProvider.GetThemeColor("dim"),
			Italic:     true,
			MarginTop:  1,
		},
	)

	content := c.styleProvider.JoinVertical(
		title,
		planHeader,
		planContent,
		"",
		c.styleProvider.PlaceCenterTop(modalWidth, c.styleProvider.GetHeight(options), options),
		helpText,
	)

	modal := c.styleProvider.RenderModal(content, modalWidth)
	return c.styleProvider.PlaceCenter(c.width, c.height, modal)
}

// formatPlanContent formats the plan content for display
func (c *PlanApprovalComponent) formatPlanContent(content string, maxWidth int) string {
	if content == "" {
		return c.styleProvider.RenderStyledText("(No plan content)", styles.StyleOptions{
			Foreground: c.styleProvider.GetThemeColor("dim"),
			Italic:     true,
		})
	}

	// Truncate if too long
	maxLines := 10
	lines := strings.Split(content, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "...")
	}

	truncatedContent := strings.Join(lines, "\n")

	// Wrap text to fit width
	wrapped := c.wrapText(truncatedContent, maxWidth)

	return c.styleProvider.RenderStyledText(wrapped, styles.StyleOptions{
		Foreground: c.styleProvider.GetThemeColor("text"),
		Width:      maxWidth,
	})
}

// wrapText wraps text to fit within maxWidth
func (c *PlanApprovalComponent) wrapText(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}

	lines := strings.Split(text, "\n")
	var wrappedLines []string

	for _, line := range lines {
		if len(line) <= maxWidth {
			wrappedLines = append(wrappedLines, line)
			continue
		}

		// Simple word wrapping
		words := strings.Fields(line)
		if len(words) == 0 {
			wrappedLines = append(wrappedLines, line[:maxWidth])
			continue
		}

		currentLine := ""
		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if len(currentLine)+1+len(word) <= maxWidth {
				currentLine += " " + word
			} else {
				wrappedLines = append(wrappedLines, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			wrappedLines = append(wrappedLines, currentLine)
		}
	}

	return strings.Join(wrappedLines, "\n")
}

// renderOptions renders the Accept/Reject/Accept & Auto-Approve options
func (c *PlanApprovalComponent) renderOptions(selectedIndex int) string {
	isAcceptSelected := selectedIndex == int(domain.PlanApprovalAccept)
	isRejectSelected := selectedIndex == int(domain.PlanApprovalReject)
	isAcceptAndAutoApproveSelected := selectedIndex == int(domain.PlanApprovalAcceptAndAutoApprove)

	var acceptIcon, rejectIcon, acceptAndAutoApproveIcon string

	if isAcceptSelected {
		acceptIcon = "✓ "
	}
	if isRejectSelected {
		rejectIcon = "✗ "
	}
	if isAcceptAndAutoApproveSelected {
		acceptAndAutoApproveIcon = "⚡ "
	}

	acceptText := acceptIcon + "Accept"
	rejectText := rejectIcon + "Reject"
	acceptAndAutoApproveText := acceptAndAutoApproveIcon + "Auto-Approve"

	acceptButton := c.styleProvider.RenderApprovalButton(acceptText, isAcceptSelected, true)
	rejectButton := c.styleProvider.RenderApprovalButton(rejectText, isRejectSelected, false)
	acceptAndAutoApproveButton := c.styleProvider.RenderApprovalButton(acceptAndAutoApproveText, isAcceptAndAutoApproveSelected, true)

	return c.styleProvider.JoinHorizontal(acceptButton, "  ", rejectButton, "  ", acceptAndAutoApproveButton)
}
