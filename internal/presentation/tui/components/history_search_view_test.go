package components

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	ansi "github.com/charmbracelet/x/ansi"

	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

func newTestHistorySearchView() *HistorySearchView {
	v := NewHistorySearchView(styles.NewProvider(styles.NewThemeProvider()))
	v.SetWidth(80)
	return v
}

func charKey(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(text)[0], Text: text}
}

func matchEntries(v *HistorySearchView) []string {
	entries := make([]string, 0, len(v.matches))
	for _, m := range v.matches {
		entries = append(entries, m.entry)
	}
	return entries
}

func TestHistorySearchOpensNewestFirstWithDuplicatesCollapsed(t *testing.T) {
	v := newTestHistorySearchView()
	v.Open([]string{"first prompt", "second prompt", "dup prompt", "third prompt", "dup prompt"})

	want := []string{"dup prompt", "third prompt", "second prompt", "first prompt"}
	if got := matchEntries(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v (most recent occurrence leads)", got, want)
	}
	if v.selected != 0 {
		t.Fatalf("initial selection = %d, want 0", v.selected)
	}
	if !v.IsVisible() || v.GetHeight() <= 0 || v.Render() == "" {
		t.Fatal("open overlay should be visible with a non-empty render")
	}
}

func TestHistorySearchClosedRendersNothing(t *testing.T) {
	v := newTestHistorySearchView()
	v.Open([]string{"a prompt"})
	v.Close()

	if v.IsVisible() {
		t.Fatal("overlay should be closed after Close")
	}
	if got := v.Render(); got != "" {
		t.Fatalf("closed Render() = %q, want \"\"", got)
	}
	if got := v.GetHeight(); got != 0 {
		t.Fatalf("closed GetHeight() = %d, want 0", got)
	}
}

func TestHistorySearchFuzzyFiltersCaseInsensitive(t *testing.T) {
	v := newTestHistorySearchView()
	v.Open([]string{"Fix FOO bug", "another prompt"})

	for _, char := range []string{"f", "f", "o"} {
		_, _ = v.HandleKey(charKey(char))
	}

	if got := matchEntries(v); len(got) != 1 || got[0] != "Fix FOO bug" {
		t.Fatalf("query ffo entries = %v, want [Fix FOO bug]", got)
	}
	if len(v.matches[0].indexes) != 3 {
		t.Fatalf("matched indexes = %v, want 3 entries", v.matches[0].indexes)
	}
}

func TestHistorySearchHighlightsMatchedCharacters(t *testing.T) {
	on := ansi.NewStyle().Bold().Underline(true).String()

	v := newTestHistorySearchView()
	v.Open([]string{"fix the build error"})

	if strings.Contains(v.Render(), on) {
		t.Fatal("empty query must not highlight")
	}

	for _, char := range []string{"t", "h", "e"} {
		_, _ = v.HandleKey(charKey(char))
	}

	if !strings.Contains(v.Render(), on) {
		t.Fatalf("matched characters should be highlighted in the rendered rows\n--- output ---\n%s", v.Render())
	}

	row := v.matches[0].oneline
	for pos, index := range v.matches[0].indexes {
		if index >= len(row) {
			t.Fatalf("matched index %d out of range for %q", index, row)
		}
		if string(row[index]) != string("the"[pos]) {
			t.Fatalf("matched index %d = %q, want %q at %d", index, row[index], "the"[pos], pos)
		}
	}
}

func TestHistorySearchSelectionWraps(t *testing.T) {
	v := newTestHistorySearchView()
	v.Open([]string{"one prompt", "two prompt", "three prompt"})

	if v.selected != 0 {
		t.Fatalf("initial selected = %d, want 0 (newest first)", v.selected)
	}

	_, _ = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if v.selected != 1 {
		t.Fatalf("after down selected = %d, want 1", v.selected)
	}

	_, _ = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	_, _ = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if v.selected != 0 {
		t.Fatalf("down past the last row wraps to %d, want 0", v.selected)
	}

	_, _ = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if v.selected != 2 {
		t.Fatalf("up past the first row wraps to %d, want 2", v.selected)
	}

	_, _ = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	_, _ = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if v.selected != 0 {
		t.Fatalf("two more ups wrap down to %d, want 0", v.selected)
	}
}

func TestHistorySearchEnterLoadsOriginalMultilinePrompt(t *testing.T) {
	v := newTestHistorySearchView()
	v.Open([]string{"other prompt", "first line\nsecond line"})

	out := v.Render()
	if !strings.Contains(out, "first line ↵ second line") {
		t.Fatalf("multi-line prompt should display on one line\n--- output ---\n%s", out)
	}

	outcome, entry := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if outcome != HistorySearchAccepted {
		t.Fatalf("enter outcome = %d, want HistorySearchAccepted", outcome)
	}
	if entry != "first line\nsecond line" {
		t.Fatalf("enter entry = %q, want the original multi-line prompt", entry)
	}
	if v.IsVisible() {
		t.Fatal("overlay should close after accepting")
	}
}

func TestHistorySearchEscClosesAndSwallowsFurtherKeys(t *testing.T) {
	v := newTestHistorySearchView()
	v.Open([]string{"alpha prompt"})

	_, _ = v.HandleKey(charKey("a"))
	outcome, entry := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if outcome != HistorySearchClosed || entry != "" {
		t.Fatalf("esc outcome = %d entry %q, want HistorySearchClosed \"\"", outcome, entry)
	}
	if v.IsVisible() {
		t.Fatal("overlay should be closed after esc")
	}
	if got := v.Render(); got != "" {
		t.Fatalf("closed overlay must render nothing, got %q", got)
	}

	outcome, entry = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if outcome != HistorySearchUnhandledKey || entry != "" {
		t.Fatalf("enter after close outcome = %d entry %q, want HistorySearchUnhandledKey \"\"", outcome, entry)
	}
}

func TestHistorySearchNoMatchesStaysOpenAndBackspaceRecovers(t *testing.T) {
	v := newTestHistorySearchView()
	v.Open([]string{"alpha prompt"})

	for _, char := range []string{"z", "z", "z"} {
		_, _ = v.HandleKey(charKey(char))
	}
	if len(v.matches) != 0 || len(v.entries) != 1 {
		t.Fatalf("matches = %d entries = %d, want 0 and 1", len(v.matches), len(v.entries))
	}

	render := v.Render()
	if !strings.Contains(render, "no matches") {
		t.Fatalf("empty result render should explain the empty state\n--- output ---\n%s", render)
	}

	outcome, entry := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if outcome != HistorySearchUnhandledKey || entry != "" {
		t.Fatalf("enter with no matches outcome = %d entry %q, want HistorySearchUnhandledKey \"\"", outcome, entry)
	}

	for i := 0; i < len("zzz"); i++ {
		_, _ = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if len(v.matches) == 0 {
		t.Fatal("backspacing the whole query should restore the unfiltered list")
	}
}

func TestHistorySearchDedupesTrimmedDuplicates(t *testing.T) {
	v := newTestHistorySearchView()
	v.Open([]string{"same prompt", "same prompt ", "same prompt"})

	want := []string{"same prompt"}
	if got := matchEntries(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}
