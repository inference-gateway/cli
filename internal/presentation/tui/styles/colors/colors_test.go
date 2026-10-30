package colors

import (
	"reflect"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

func TestColor_GetLipglossColor(t *testing.T) {
	c := Color{ANSI: Red, Lipgloss: "31"}
	got := c.GetLipglossColor()
	if got == nil {
		t.Fatal("expected non-nil color")
	}
	want := lipgloss.Color("31")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expected lipgloss color from %q to deeply equal %#v, got %#v", "31", want, got)
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

func TestCreateColoredText_EmptyText(t *testing.T) {
	result := CreateColoredText("", ErrorColor)

	if !strings.Contains(result, ErrorColor.ANSI) {
		t.Error("Expected result to contain color code even with empty text")
	}

	if !strings.Contains(result, Reset) {
		t.Error("Expected result to contain reset code even with empty text")
	}
}
