package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestColor_GetLipglossColor(t *testing.T) {
	color := Color{ANSI: ColorRed, Lipgloss: "31"}

	lipglossColor := color.GetLipglossColor()

	if lipglossColor != lipgloss.Color("31") {
		t.Errorf("Expected lipgloss color '31', got '%s'", string(lipglossColor))
	}
}

func TestReset(t *testing.T) {
	reset := Reset()

	if reset != ColorReset {
		t.Errorf("Expected reset code '%s', got '%s'", ColorReset, reset)
	}
}

func TestPredefinedColors(t *testing.T) {
	testCases := []struct {
		name     string
		color    Color
		expected string
	}{
		{"UserColor", UserColor, ColorBlue},
		{"AssistantColor", AssistantColor, ColorWhite},
		{"ErrorColor", ErrorColor, ColorRed},
		{"StatusColor", StatusColor, ColorMagenta},
		{"AccentColor", AccentColor, ColorBlue},
		{"DimColor", DimColor, ColorGray},
		{"BorderColor", BorderColor, ColorGray},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.color.ANSI != tc.expected {
				t.Errorf("Expected %s ANSI color '%s', got '%s'", tc.name, tc.expected, tc.color.ANSI)
			}

			if tc.color.Lipgloss == "" {
				t.Errorf("Expected %s to have non-empty Lipgloss color", tc.name)
			}
		})
	}
}

func TestNewCommonStyles(t *testing.T) {
	styles := NewCommonStyles()

	if styles == nil {
		t.Fatal("Expected CommonStyles to be created, got nil")
	}

	testCases := []struct {
		name  string
		style lipgloss.Style
	}{
		{"Header", styles.Header},
		{"Border", styles.Border},
		{"Separator", styles.Separator},
		{"Input", styles.Input},
		{"Status", styles.Status},
		{"HelpBar", styles.HelpBar},
		{"Conversation", styles.Conversation},
		{"BashIndicator", styles.BashIndicator},
		{"ModelIndicator", styles.ModelIndicator},
		{"PlaceholderText", styles.PlaceholderText},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rendered := tc.style.Render("test")
			if rendered == "" {
				t.Errorf("Expected %s style to render non-empty content", tc.name)
			}
		})
	}
}

func TestCreateSeparator(t *testing.T) {
	separator := CreateSeparator(5, "-")

	if !strings.Contains(separator, "-----") {
		t.Error("Expected separator to contain repeated character")
	}

	if !strings.Contains(separator, DimColor.ANSI) {
		t.Error("Expected separator to contain color code")
	}

	if !strings.Contains(separator, Reset()) {
		t.Error("Expected separator to contain reset code")
	}
}

func TestCreateColoredText(t *testing.T) {
	text := "Hello, World!"
	color := UserColor

	result := CreateColoredText(text, color)

	if !strings.Contains(result, text) {
		t.Error("Expected result to contain original text")
	}

	if !strings.Contains(result, color.ANSI) {
		t.Error("Expected result to contain color code")
	}

	if !strings.Contains(result, Reset()) {
		t.Error("Expected result to contain reset code")
	}
}

func TestCreateStyledText(t *testing.T) {
	text := "Styled text"
	colorCode := ColorRed

	result := CreateStyledText(text, colorCode)

	if !strings.Contains(result, text) {
		t.Error("Expected result to contain original text")
	}

	if !strings.Contains(result, colorCode) {
		t.Error("Expected result to contain color code")
	}

	if !strings.Contains(result, Reset()) {
		t.Error("Expected result to contain reset code")
	}
}

func TestCreateSeparator_EmptyChar(t *testing.T) {
	separator := CreateSeparator(3, "")

	if !strings.Contains(separator, DimColor.ANSI) {
		t.Error("Expected separator to still contain color code with empty char")
	}
}

func TestCreateSeparator_ZeroWidth(t *testing.T) {
	separator := CreateSeparator(0, "-")

	if !strings.Contains(separator, DimColor.ANSI) {
		t.Error("Expected separator to still contain color code with zero width")
	}

	if !strings.Contains(separator, Reset()) {
		t.Error("Expected separator to contain reset code with zero width")
	}
}

func TestCreateColoredText_EmptyText(t *testing.T) {
	result := CreateColoredText("", ErrorColor)

	if !strings.Contains(result, ErrorColor.ANSI) {
		t.Error("Expected result to contain color code even with empty text")
	}

	if !strings.Contains(result, Reset()) {
		t.Error("Expected result to contain reset code even with empty text")
	}
}

func TestCommonStyles_Chaining(t *testing.T) {
	styles := NewCommonStyles()

	customHeader := styles.Header.Background(lipgloss.Color("blue"))
	rendered := customHeader.Render("Test Header")

	if rendered == "" {
		t.Error("Expected chained style to render content")
	}
}
