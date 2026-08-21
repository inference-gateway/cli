package diffview

import (
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

func TestNewThemeAwareStyle_OverridesForegroundColours(t *testing.T) {
	s := NewThemeAwareStyle(ThemePalette{
		Add:    "#11ff22",
		Remove: "#ff3344",
		Accent: "#5566ff",
		Dim:    "#777777",
		Dark:   true,
	})

	if got := s.InsertLine.Symbol.GetForeground(); got != lipgloss.Color("#11ff22") {
		t.Errorf("insert symbol fg = %v, want #11ff22", got)
	}
	if got := s.DeleteLine.Symbol.GetForeground(); got != lipgloss.Color("#ff3344") {
		t.Errorf("delete symbol fg = %v, want #ff3344", got)
	}
	if got := s.DividerLine.Code.GetForeground(); got != lipgloss.Color("#5566ff") {
		t.Errorf("divider code fg = %v, want #5566ff", got)
	}
	if got := s.EqualLine.LineNumber.GetForeground(); got != lipgloss.Color("#777777") {
		t.Errorf("equal line-number fg = %v, want #777777", got)
	}
}

func TestNewThemeAwareStyle_SelectsBaseByDarkFlag(t *testing.T) {
	dark := NewThemeAwareStyle(ThemePalette{Dark: true})
	light := NewThemeAwareStyle(ThemePalette{Dark: false})

	if dark.EqualLine.Code.GetBackground() == light.EqualLine.Code.GetBackground() {
		t.Error("expected dark and light bases to have different code backgrounds")
	}
}

func TestNewThemeAwareStyle_EmptyPaletteKeepsTunedBase(t *testing.T) {
	base := DefaultDarkStyle()
	got := NewThemeAwareStyle(ThemePalette{Dark: true})

	if got.InsertLine.Symbol.GetForeground() != base.InsertLine.Symbol.GetForeground() {
		t.Error("empty palette should keep the tuned base insert foreground")
	}
	if got.InsertLine.Code.GetBackground() != base.InsertLine.Code.GetBackground() {
		t.Error("background tints should always be inherited from the base")
	}
}
