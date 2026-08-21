package icons

import (
	lipgloss "charm.land/lipgloss/v2"

	colors "github.com/inference-gateway/cli/internal/presentation/tui/styles/colors"
)

// Status icons
const (
	CheckMark = "✓"
	CrossMark = "✗"
	GitBranch = "⎇"
)

// Emoji icons
const (
	Robot = "🤖"
	Link  = "🔗"
	Help  = "❓"
)

// Tool execution icons - modern Unicode symbols
const (
	QueuedIcon = "•"
	BulletIcon = "•"
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
