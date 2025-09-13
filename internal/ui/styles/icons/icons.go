package icons

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// Status icons
const (
	CheckMark = "✓"
	CrossMark = "✗"
)

// Tool execution icons
const (
	QueuedIcon    = "○"
	ExecutingIcon = "●"
	BulletIcon    = "•"
)

// Icon styles
var (
	CheckMarkStyle = lipgloss.NewStyle().Foreground(colors.SuccessColor.GetLipglossColor()).Bold(true)
	CrossMarkStyle = lipgloss.NewStyle().Foreground(colors.ErrorColor.GetLipglossColor()).Bold(true)
)

// Helper functions for consistent colored icon usage
func StyledCheckMark() string {
	return CheckMarkStyle.Render(CheckMark)
}

func StyledCrossMark() string {
	return CrossMarkStyle.Render(CrossMark)
}
