package styles

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// CommonStyles contains reusable lipgloss styles
type CommonStyles struct {
	Header          lipgloss.Style
	Border          lipgloss.Style
	Separator       lipgloss.Style
	Input           lipgloss.Style
	Status          lipgloss.Style
	HelpBar         lipgloss.Style
	Conversation    lipgloss.Style
	BashIndicator   lipgloss.Style
	ModelIndicator  lipgloss.Style
	PlaceholderText lipgloss.Style
}

// NewCommonStyles creates a new set of common styles with consistent theming
func NewCommonStyles() *CommonStyles {
	return &CommonStyles{
		Header: lipgloss.NewStyle().
			Align(lipgloss.Center).
			Foreground(colors.HeaderColor.GetLipglossColor()).
			Bold(true).
			Padding(0, 1),
		Border: lipgloss.NewStyle().
			Foreground(colors.BorderColor.GetLipglossColor()),
		Separator: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()),
		Input: lipgloss.NewStyle(),
		Status: lipgloss.NewStyle().
			Foreground(colors.SpinnerColor.GetLipglossColor()),
		HelpBar: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()).
			Bold(true),
		Conversation: lipgloss.NewStyle(),
		BashIndicator: lipgloss.NewStyle().
			Foreground(colors.StatusColor.GetLipglossColor()).
			Bold(true),
		ModelIndicator: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()).
			Bold(true),
		PlaceholderText: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()),
	}
}

// RoundedBorder returns a rounded border style for lipgloss
func RoundedBorder() lipgloss.Border {
	return lipgloss.RoundedBorder()
}
