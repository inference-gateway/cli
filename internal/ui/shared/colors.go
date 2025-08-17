package shared

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ANSI Color Codes
const (
	ColorReset         = "\033[0m"
	ColorRed           = "\033[31m"
	ColorGreen         = "\033[32m"
	ColorBlue          = "\033[34m"
	ColorCyan          = "\033[36m"
	ColorMagenta       = "\033[35m"
	ColorWhite         = "\033[37m"
	ColorGray          = "\033[90m"
	ColorBrightRed     = "\033[91m"
	ColorStrikethrough = "\033[9m"
	ColorDim           = "\033[2m"
)

// Lipgloss Color Names
const (
	LipglossBlue    = "34"
	LipglossGray    = "240"
	LipglossCyan    = "39"
	LipglossMagenta = "205"
)

// Color represents a color that can be used in both ANSI and Lipgloss contexts
type Color struct {
	ANSI     string
	Lipgloss string
}

// Predefined colors for consistent theming
var (
	UserColor      = Color{ANSI: ColorCyan, Lipgloss: "36"}
	AssistantColor = Color{ANSI: ColorGreen, Lipgloss: "32"}
	ErrorColor     = Color{ANSI: ColorRed, Lipgloss: "31"}
	StatusColor    = Color{ANSI: ColorBlue, Lipgloss: LipglossBlue}
	AccentColor    = Color{ANSI: ColorMagenta, Lipgloss: "35"}
	DimColor       = Color{ANSI: ColorGray, Lipgloss: LipglossGray}
	BorderColor    = Color{ANSI: ColorWhite, Lipgloss: "37"}
	HeaderColor    = Color{ANSI: ColorCyan, Lipgloss: LipglossCyan}
	SpinnerColor   = Color{ANSI: ColorMagenta, Lipgloss: LipglossMagenta}
)

// GetLipglossColor returns a lipgloss color for the given Color
func (c Color) GetLipglossColor() lipgloss.Color {
	return lipgloss.Color(c.Lipgloss)
}

// Reset returns the ANSI reset code
func Reset() string {
	return ColorReset
}

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
			Foreground(HeaderColor.GetLipglossColor()).
			Bold(true).
			Padding(0, 1),
		Border: lipgloss.NewStyle().
			Foreground(BorderColor.GetLipglossColor()),
		Separator: lipgloss.NewStyle().
			Foreground(DimColor.GetLipglossColor()),
		Input: lipgloss.NewStyle(),
		Status: lipgloss.NewStyle().
			Foreground(SpinnerColor.GetLipglossColor()),
		HelpBar: lipgloss.NewStyle().
			Foreground(DimColor.GetLipglossColor()).
			Bold(true),
		Conversation: lipgloss.NewStyle(),
		BashIndicator: lipgloss.NewStyle().
			Foreground(StatusColor.GetLipglossColor()).
			Bold(true),
		ModelIndicator: lipgloss.NewStyle().
			Foreground(DimColor.GetLipglossColor()).
			Bold(true),
		PlaceholderText: lipgloss.NewStyle().
			Foreground(DimColor.GetLipglossColor()),
	}
}

// Helper functions for common styling patterns

// CreateSeparator creates a separator line with the given width and character
func CreateSeparator(width int, char string) string {
	return DimColor.ANSI + strings.Repeat(char, width) + Reset()
}

// CreateColoredText creates colored text with automatic reset
func CreateColoredText(text string, color Color) string {
	return color.ANSI + text + Reset()
}

// CreateStyledText creates text with color and reset, commonly used pattern
func CreateStyledText(text, colorCode string) string {
	return colorCode + text + Reset()
}

// CreateStrikethroughText creates text with strikethrough styling
func CreateStrikethroughText(text string) string {
	return ColorStrikethrough + ColorGray + text + Reset()
}

// CreateDimText creates text with dim/faint styling
func CreateDimText(text string) string {
	return ColorDim + text + Reset()
}
