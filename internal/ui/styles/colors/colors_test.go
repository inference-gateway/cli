package colors

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestColor_GetLipglossColor(t *testing.T) {
	color := Color{ANSI: Red, Lipgloss: "31"}

	lipglossColor := color.GetLipglossColor()

	if lipglossColor != lipgloss.Color("31") {
		t.Errorf("Expected lipgloss color '31', got '%s'", string(lipglossColor))
	}
}

func TestReset(t *testing.T) {
	reset := Reset

	if reset != Reset {
		t.Errorf("Expected reset code '%s', got '%s'", Reset, reset)
	}
}

func TestPredefinedColors(t *testing.T) {
	testCases := []struct {
		name         string
		color        Color
		expectedANSI string
	}{
		{"UserColor", UserColor, Blue},
		{"AssistantColor", AssistantColor, White},
		{"ErrorColor", ErrorColor, Red},
		{"StatusColor", StatusColor, Magenta},
		{"AccentColor", AccentColor, Blue},
		{"DimColor", DimColor, Gray},
		{"BorderColor", BorderColor, Gray},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.color.ANSI != tc.expectedANSI {
				t.Errorf("Expected %s ANSI color '%s', got '%s'", tc.name, tc.expectedANSI, tc.color.ANSI)
			}

			if tc.color.Lipgloss == "" {
				t.Errorf("Expected %s to have non-empty Lipgloss color", tc.name)
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

	if !strings.Contains(separator, Reset) {
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

	if !strings.Contains(result, Reset) {
		t.Error("Expected result to contain reset code")
	}
}

func TestCreateStyledText(t *testing.T) {
	text := "Styled text"
	colorCode := Red

	result := CreateStyledText(text, colorCode)

	if !strings.Contains(result, text) {
		t.Error("Expected result to contain original text")
	}

	if !strings.Contains(result, colorCode) {
		t.Error("Expected result to contain color code")
	}

	if !strings.Contains(result, Reset) {
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

	if !strings.Contains(separator, Reset) {
		t.Error("Expected separator to contain reset code with zero width")
	}
}

func TestCreateColoredText_EmptyText(t *testing.T) {
	result := CreateColoredText("", ErrorColor)

	if !strings.Contains(result, ErrorColor.ANSI) {
		t.Error("Expected result to contain color code even with empty text")
	}

	if !strings.Contains(result, Reset) {
		t.Error("Expected result to contain reset code even with empty text")
	}
}
