package shared

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ANSI Color Codes - Tokyo Night Theme
const (
	ColorReset         = "\033[0m"
	ColorRed           = "\033[38;2;247;118;142m" // #f7768e - soft red for errors
	ColorGreen         = "\033[38;2;158;206;106m" // #9ece6a - green for success
	ColorBlue          = "\033[38;2;122;162;247m" // #7aa2f7 - blue for accent
	ColorCyan          = "\033[38;2;125;207;255m" // #7dcfff - cyan variant
	ColorMagenta       = "\033[38;2;187;154;247m" // #bb9af7 - purple for secondary
	ColorWhite         = "\033[38;2;169;177;214m" // #a9b1d6 - light gray-blue for primary text
	ColorGray          = "\033[38;2;86;95;137m"   // #565f89 - dim gray
	ColorAmber         = "\033[38;2;224;175;104m" // #e0af68 - amber for warnings
	ColorBrightRed     = "\033[38;2;247;118;142m" // Same as ColorRed
	ColorStrikethrough = "\033[9m"
	ColorDim           = "\033[2m"
	ColorLightGreenBg  = "\033[48;2;34;139;34m" // More vibrant green background for diff overlays
	ColorLightRedBg    = "\033[48;2;220;20;60m" // More vibrant red background for diff overlays
)

// Lipgloss Color Names - Tokyo Night Theme Hex Values
const (
	LipglossRed     = "#f7768e"
	LipglossGreen   = "#9ece6a"
	LipglossBlue    = "#7aa2f7"
	LipglossCyan    = "#7dcfff"
	LipglossMagenta = "#bb9af7"
	LipglossWhite   = "#a9b1d6"
	LipglossGray    = "#565f89"
	LipglossAmber   = "#e0af68"
)

// Color represents a color that can be used in both ANSI and Lipgloss contexts
type Color struct {
	ANSI     string
	Lipgloss string
}

// Predefined colors for consistent theming - Tokyo Night Theme
var (
	UserColor       = Color{ANSI: ColorBlue, Lipgloss: LipglossBlue}       // Blue for user prompts
	AssistantColor  = Color{ANSI: ColorWhite, Lipgloss: LipglossWhite}     // Light gray-blue for assistant
	ErrorColor      = Color{ANSI: ColorRed, Lipgloss: LipglossRed}         // Soft red for errors
	SuccessColor    = Color{ANSI: ColorGreen, Lipgloss: LipglossGreen}     // Green for success
	StatusColor     = Color{ANSI: ColorMagenta, Lipgloss: LipglossMagenta} // Purple for status/info
	AccentColor     = Color{ANSI: ColorBlue, Lipgloss: LipglossBlue}       // Blue for accents
	DimColor        = Color{ANSI: ColorGray, Lipgloss: LipglossGray}       // Dim gray
	BorderColor     = Color{ANSI: ColorGray, Lipgloss: LipglossGray}       // Gray for borders
	HeaderColor     = Color{ANSI: ColorBlue, Lipgloss: LipglossBlue}       // Blue for headers
	SpinnerColor    = Color{ANSI: ColorMagenta, Lipgloss: LipglossMagenta} // Purple for spinners
	DiffAddColor    = Color{ANSI: ColorGreen, Lipgloss: LipglossGreen}     // Green for additions
	DiffRemoveColor = Color{ANSI: ColorRed, Lipgloss: LipglossRed}         // Red for removals
	WarningColor    = Color{ANSI: ColorAmber, Lipgloss: LipglossAmber}     // Amber for warnings
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

// RoundedBorder returns a rounded border style for lipgloss
func RoundedBorder() lipgloss.Border {
	return lipgloss.RoundedBorder()
}

// Diff formatting helpers

// CreateDiffAddedLine creates a diff line for added content with line number
func CreateDiffAddedLine(lineNum int, content string) string {
	return createDiffLineWithBg("+", lineNum, content, ColorLightGreenBg)
}

// CreateDiffRemovedLine creates a diff line for removed content with line number
func CreateDiffRemovedLine(lineNum int, content string) string {
	return createDiffLineWithBg("-", lineNum, content, ColorLightRedBg)
}

// createDiffLineWithBg creates a diff line with background only covering content
func createDiffLineWithBg(prefix string, lineNum int, content string, bgColor string) string {
	lineNumStr := ColorGray + formatLineNumber(lineNum) + Reset()

	brightWhite := "\033[1;37m\033[38;2;255;255;255m"
	contentPart := brightWhite + prefix + " " + content

	return lineNumStr + " " + bgColor + contentPart + Reset()
}

// CreateDiffUnchangedLine creates a diff line for unchanged content with line number
func CreateDiffUnchangedLine(lineNum int, content string) string {
	return ColorGray + formatLineNumber(lineNum) + Reset() + "  " + content
}

// formatLineNumber formats a line number to a 3-character right-aligned string
func formatLineNumber(lineNum int) string {
	return fmt.Sprintf("%3d", lineNum)
}
