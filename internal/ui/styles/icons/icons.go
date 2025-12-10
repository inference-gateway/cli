package icons

import (
	"github.com/charmbracelet/lipgloss"
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
	SpinnerFrames = "⣾⣽⣻⢿⡿⣟⣯⣷"
	ProgressDots  = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	ModernSpinner = "◐◓◑◒"
	ArrowSpinner  = "▶▷▶▷"
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

// GetSpinnerFrame returns the appropriate spinner frame based on animation step
func GetSpinnerFrame(step int) string {
	frames := ModernSpinner
	runes := []rune(frames)
	if len(runes) == 0 {
		return ExecutingIcon
	}
	return string(runes[step%len(runes)])
}
