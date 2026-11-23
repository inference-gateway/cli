package components

import (
	"strings"
	"testing"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	lipgloss "charm.land/lipgloss/v2"
	ansi "github.com/charmbracelet/x/ansi"

	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

func newTestPicker() *Picker {
	theme := &tuimocks.FakeTheme{}
	theme.GetAccentColorReturns("#ff9e64")
	theme.GetDimColorReturns("#888888")
	themeService := &tuimocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(theme)
	return NewPicker(styles.NewProvider(themeService))
}

var (
	testPickerSpec = PickerSpec{Title: "Switch agent", Hint: "the next message runs as it", Keys: "↑/↓ move · enter switch · esc close"}
	testPickerRows = []PickerRow{
		{Value: "main", Label: "main", Tag: "read-write", TagColor: "warning", Detail: "The default main agent."},
		{Value: "explore", Label: "explore", Tag: "read-only", TagColor: "success", Detail: "Read-only search agent."},
		{Value: "general-purpose", Label: "general-purpose", Tag: "read-write", TagColor: "warning", Detail: strings.Repeat("A long description. ", 20)},
	}
)

func TestPickerOpensOnTheCurrentValueAndWraps(t *testing.T) {
	p := newTestPicker()
	p.Open(testPickerSpec, testPickerRows, "explore")

	if !p.IsOpen() || p.Selected() != "explore" {
		t.Fatalf("opened on %q, want the current value explore", p.Selected())
	}
	p.Move(-1)
	if p.Selected() != "main" {
		t.Fatalf("the row above explore is %q, want main as given", p.Selected())
	}
	p.Move(-1)
	if p.Selected() != "general-purpose" {
		t.Fatalf("moving up from the top must wrap to the last row, got %q", p.Selected())
	}
	p.Close()
	if p.IsOpen() || p.Render() != "" {
		t.Fatal("a closed picker renders nothing")
	}
}

func TestPickerRendersWithinItsWidth(t *testing.T) {
	p := newTestPicker()
	p.Open(testPickerSpec, testPickerRows, "main")
	p.SetWidth(300)

	out := p.Render()
	for _, want := range []string{"Switch agent", "read-only", "read-write", "enter switch"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q", want)
		}
	}
	if width := lipgloss.Width(out); width > pickerMaxInner+pickerChrome {
		t.Fatalf("render is %d wide, want the detail truncated to at most %d", width, pickerMaxInner+pickerChrome)
	}

	p.SetWidth(60)
	if width := lipgloss.Width(p.Render()); width > 60 {
		t.Fatalf("render is %d wide in a 60 column row", width)
	}
}

func TestPickerWithoutTagsKeepsNoTagColumn(t *testing.T) {
	p := newTestPicker()
	p.Open(PickerSpec{Title: "Switch room", Keys: "esc close"}, []PickerRow{
		{Value: "", Label: "No room"},
		{Value: "release", Label: "# release", Detail: "2 members"},
	}, "")

	out := ansi.Strip(p.Render())
	if !strings.Contains(out, "# release  2 members") {
		t.Fatalf("render %q lacks the detail right after the label", out)
	}
	if p.Selected() != "" {
		t.Fatalf("opened on %q, want No room, the current value", p.Selected())
	}
}

func TestPickerWithNoRowsStaysClosed(t *testing.T) {
	p := newTestPicker()
	p.Open(testPickerSpec, nil, "main")
	if p.IsOpen() || p.Selected() != "" {
		t.Fatal("with no rows to list the picker must not open")
	}
}
