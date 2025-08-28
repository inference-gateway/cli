package colors

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ANSI Color Codes - Tokyo Night Theme
const (
	Reset         = "\033[0m"
	Red           = "\033[38;2;247;118;142m" // #f7768e - soft red for errors
	Green         = "\033[38;2;158;206;106m" // #9ece6a - green for success
	Blue          = "\033[38;2;122;162;247m" // #7aa2f7 - blue for accent
	Cyan          = "\033[38;2;125;207;255m" // #7dcfff - cyan variant
	Magenta       = "\033[38;2;187;154;247m" // #bb9af7 - purple for secondary
	White         = "\033[38;2;169;177;214m" // #a9b1d6 - light gray-blue for primary text
	Gray          = "\033[38;2;86;95;137m"   // #565f89 - dim gray
	Amber         = "\033[38;2;224;175;104m" // #e0af68 - amber for warnings
	BrightRed     = "\033[38;2;247;118;142m" // Same as Red
	Strikethrough = "\033[9m"
	Dim           = "\033[2m"
	LightGreenBg  = "\033[48;2;34;139;34m" // More vibrant green background for diff overlays
	LightRedBg    = "\033[48;2;220;20;60m" // More vibrant red background for diff overlays
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
	LipglossBlack   = "#000000"
	LipglossWhiteBg = "#FFFFFF"

	// GitHub Light Theme Colors
	GithubBlue      = "#0366d6"
	GithubDarkGray  = "#24292e"
	GithubRed       = "#d73a49"
	GithubPurple    = "#8250df"
	GithubGray      = "#586069"
	GithubLightGray = "#d0d7de"
	GithubGreen     = "#28a745"

	// Dracula Theme Colors
	DraculaCyan       = "#8be9fd"
	DraculaForeground = "#f8f8f2"
	DraculaRed        = "#ff5555"
	DraculaPurple     = "#bd93f9"
	DraculaPink       = "#ff79c6"
	DraculaComment    = "#6272a4"
	DraculaSelection  = "#44475a"
	DraculaGreen      = "#50fa7b"
)

// Color represents a color that can be used in both ANSI and Lipgloss contexts
type Color struct {
	ANSI     string
	Lipgloss string
}

// Predefined colors for consistent theming - Tokyo Night Theme
var (
	UserColor               = Color{ANSI: Blue, Lipgloss: LipglossBlue}                        // Blue for user prompts
	AssistantColor          = Color{ANSI: White, Lipgloss: LipglossWhite}                      // Light gray-blue for assistant
	ErrorColor              = Color{ANSI: Red, Lipgloss: LipglossRed}                          // Soft red for errors
	SuccessColor            = Color{ANSI: Green, Lipgloss: LipglossGreen}                      // Green for success
	StatusColor             = Color{ANSI: Magenta, Lipgloss: LipglossMagenta}                  // Purple for status/info
	AccentColor             = Color{ANSI: Blue, Lipgloss: LipglossBlue}                        // Blue for accents
	DimColor                = Color{ANSI: Gray, Lipgloss: LipglossGray}                        // Dim gray
	BorderColor             = Color{ANSI: Gray, Lipgloss: LipglossGray}                        // Gray for borders
	HeaderColor             = Color{ANSI: Blue, Lipgloss: LipglossBlue}                        // Blue for headers
	SpinnerColor            = Color{ANSI: Magenta, Lipgloss: LipglossMagenta}                  // Purple for spinners
	DiffAddColor            = Color{ANSI: Green, Lipgloss: LipglossGreen}                      // Green for additions
	DiffRemoveColor         = Color{ANSI: Red, Lipgloss: LipglossRed}                          // Red for removals
	WarningColor            = Color{ANSI: Amber, Lipgloss: LipglossAmber}                      // Amber for warnings
	TextSelectionForeground = Color{ANSI: "\033[38;2;0;0;0m", Lipgloss: LipglossBlack}         // Black for text selection foreground
	TextSelectionCursor     = Color{ANSI: "\033[48;2;255;255;255m", Lipgloss: LipglossWhiteBg} // White background for text selection cursor

	// GitHub Light Theme Colors
	GithubUserColor       = Color{ANSI: "\033[38;2;3;102;214m", Lipgloss: GithubBlue}
	GithubAssistantColor  = Color{ANSI: "\033[38;2;36;41;46m", Lipgloss: GithubDarkGray}
	GithubErrorColor      = Color{ANSI: "\033[38;2;215;58;73m", Lipgloss: GithubRed}
	GithubStatusColor     = Color{ANSI: "\033[38;2;130;87;223m", Lipgloss: GithubPurple}
	GithubAccentColor     = Color{ANSI: "\033[38;2;3;102;214m", Lipgloss: GithubBlue}
	GithubDimColor        = Color{ANSI: "\033[38;2;88;96;105m", Lipgloss: GithubGray}
	GithubBorderColor     = Color{ANSI: "\033[38;2;208;215;222m", Lipgloss: GithubLightGray}
	GithubDiffAddColor    = Color{ANSI: "\033[38;2;40;167;69m", Lipgloss: GithubGreen}
	GithubDiffRemoveColor = Color{ANSI: "\033[38;2;215;58;73m", Lipgloss: GithubRed}

	// Dracula Theme Colors
	DraculaUserColor       = Color{ANSI: "\033[38;2;139;233;253m", Lipgloss: DraculaCyan}
	DraculaAssistantColor  = Color{ANSI: "\033[38;2;248;248;242m", Lipgloss: DraculaForeground}
	DraculaErrorColor      = Color{ANSI: "\033[38;2;255;85;85m", Lipgloss: DraculaRed}
	DraculaStatusColor     = Color{ANSI: "\033[38;2;189;147;249m", Lipgloss: DraculaPurple}
	DraculaAccentColor     = Color{ANSI: "\033[38;2;255;121;198m", Lipgloss: DraculaPink}
	DraculaDimColor        = Color{ANSI: "\033[38;2;98;114;164m", Lipgloss: DraculaComment}
	DraculaBorderColor     = Color{ANSI: "\033[38;2;68;71;90m", Lipgloss: DraculaSelection}
	DraculaDiffAddColor    = Color{ANSI: "\033[38;2;80;250;123m", Lipgloss: DraculaGreen}
	DraculaDiffRemoveColor = Color{ANSI: "\033[38;2;255;85;85m", Lipgloss: DraculaRed}
)

// GetLipglossColor returns a lipgloss color for the given Color
func (c Color) GetLipglossColor() lipgloss.Color {
	return lipgloss.Color(c.Lipgloss)
}

// Helper functions for common styling patterns

// CreateSeparator creates a separator line with the given width and character
func CreateSeparator(width int, char string) string {
	return DimColor.ANSI + strings.Repeat(char, width) + Reset
}

// CreateColoredText creates colored text with automatic reset
func CreateColoredText(text string, color Color) string {
	return color.ANSI + text + Reset
}

// CreateColoredTextSimple creates colored text with automatic reset using string color
func CreateColoredTextSimple(text, color string) string {
	return color + text + Reset
}

// CreateStyledText creates text with color and reset, commonly used pattern
func CreateStyledText(text, colorCode string) string {
	return colorCode + text + Reset
}

// CreateStrikethroughText creates text with strikethrough styling
func CreateStrikethroughText(text string) string {
	return Strikethrough + Gray + text + Reset
}

// CreateDimText creates text with dim/faint styling
func CreateDimText(text string) string {
	return Dim + text + Reset
}

// Diff formatting helpers

// CreateDiffAddedLine creates a diff line for added content with line number
func CreateDiffAddedLine(lineNum int, content string) string {
	return createDiffLineWithBg("+", lineNum, content, LightGreenBg)
}

// CreateDiffRemovedLine creates a diff line for removed content with line number
func CreateDiffRemovedLine(lineNum int, content string) string {
	return createDiffLineWithBg("-", lineNum, content, LightRedBg)
}

// createDiffLineWithBg creates a diff line with background only covering content
func createDiffLineWithBg(prefix string, lineNum int, content string, bgColor string) string {
	lineNumStr := Gray + formatLineNumber(lineNum) + Reset

	brightWhite := "\033[1;37m\033[38;2;255;255;255m"
	contentPart := brightWhite + prefix + " " + content

	return lineNumStr + " " + bgColor + contentPart + Reset
}

// CreateDiffUnchangedLine creates a diff line for unchanged content with line number
func CreateDiffUnchangedLine(lineNum int, content string) string {
	return Gray + formatLineNumber(lineNum) + Reset + "  " + content
}

// formatLineNumber formats a line number to a 3-character right-aligned string
func formatLineNumber(lineNum int) string {
	return fmt.Sprintf("%3d", lineNum)
}
