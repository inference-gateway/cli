package icons

import (
	"charm.land/lipgloss/v2"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// Status icons
const (
	CheckMark = "✓"
	CrossMark = "✗"
	GitBranch = "⎇"
)

// Emoji icons
const (
	Robot     = "🤖"
	Link      = "🔗"
	Help      = "❓"
	Lightbulb = "💡"
)

// Tool execution icons - modern Unicode symbols
const (
	QueuedIcon    = "•"
	ExecutingIcon = "⚡"
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
