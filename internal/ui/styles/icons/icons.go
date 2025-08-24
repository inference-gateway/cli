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

// Icon styles
var (
	CheckMarkStyle = lipgloss.NewStyle().Foreground(colors.SuccessColor.GetLipglossColor()).Bold(true)
	CrossMarkStyle = lipgloss.NewStyle().Foreground(colors.ErrorColor.GetLipglossColor()).Bold(true)
)
