package components

import (
	"fmt"
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	fuzzy "github.com/sahilm/fuzzy"

	autocomplete "github.com/inference-gateway/cli/internal/presentation/tui/autocomplete"
	keys "github.com/inference-gateway/cli/internal/presentation/tui/keys"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// maxVisibleHistoryRows caps how many history rows the overlay renders at once;
// the window scrolls to keep the selected row visible.
const maxVisibleHistoryRows = 7

// newlineDisplay replaces each newline of a multi-line prompt in list rows.
const newlineDisplay = " ↵ "

// HistorySearchOutcome reports how a key press handled by the history search
// overlay affected the chat.
type HistorySearchOutcome int

const (
	// HistorySearchUnhandledKey was consumed by the overlay, with no external effect.
	HistorySearchUnhandledKey HistorySearchOutcome = iota
	// HistorySearchAccepted selects the highlighted prompt for the input.
	HistorySearchAccepted
	// HistorySearchClosed closes the overlay, leaving the input untouched.
	HistorySearchClosed
)

var historySearchKeys = struct {
	up        key.Binding
	down      key.Binding
	accept    key.Binding
	cancel    key.Binding
	backspace key.Binding
}{
	up:        key.NewBinding(key.WithKeys("up", "ctrl+p")),
	down:      key.NewBinding(key.WithKeys("down", "ctrl+n")),
	accept:    key.NewBinding(key.WithKeys("enter")),
	cancel:    key.NewBinding(key.WithKeys("esc")),
	backspace: key.NewBinding(key.WithKeys("backspace")),
}

// HistorySearchView is the Ctrl+R fuzzy prompt-history search overlay: a query
// line plus the matching prompts, newest first, duplicates collapsed. It is a
// modal component driven by ChatApplication, which routes keys into it while
// it holds focus and renders it above the input.
type HistorySearchView struct {
	styleProvider *styles.Provider
	width         int
	open          bool
	query         []rune
	entries       []string
	oneline       []string
	matches       []historySearchMatch
	selected      int
}

// historySearchMatch is one visible row: the original prompt, its one-line
// display form, and the fuzzy-matched byte indexes to highlight.
type historySearchMatch struct {
	entry   string
	oneline string
	indexes []int
}

// NewHistorySearchView creates an overlay ready to be opened over a history.
func NewHistorySearchView(styleProvider *styles.Provider) *HistorySearchView {
	return &HistorySearchView{styleProvider: styleProvider, width: 80}
}

// Open starts a search over the project prompt history: duplicates collapse to
// their most recent occurrence and the newest prompt lists first.
func (v *HistorySearchView) Open(history []string) {
	v.entries = dedupeHistoryNewestFirst(history)
	v.oneline = make([]string, len(v.entries))
	for i, entry := range v.entries {
		v.oneline[i] = onelineHistoryEntry(entry)
	}
	v.open = true
	v.query = nil
	v.refilter()
}

// Close dismisses the overlay; the caller keeps the input exactly as it was.
func (v *HistorySearchView) Close() {
	v.open = false
	v.query = nil
}

// IsVisible reports whether the overlay is open.
func (v *HistorySearchView) IsVisible() bool { return v.open }

// Query returns the current search query.
func (v *HistorySearchView) Query() string { return string(v.query) }

// SetWidth sets the overlay width.
func (v *HistorySearchView) SetWidth(width int) { v.width = width }

// GetHeight returns the rendered line count including the border, 0 when closed.
func (v *HistorySearchView) GetHeight() int {
	lines := v.contentLines()
	if len(lines) == 0 {
		return 0
	}
	return len(lines) + 2
}

// Render returns the framed overlay, or "" when closed.
func (v *HistorySearchView) Render() string {
	lines := v.contentLines()
	if len(lines) == 0 {
		return ""
	}
	return v.styleProvider.RenderBorderedBox(strings.Join(lines, "\n"), v.styleProvider.GetThemeColor("dim"), 0, 1)
}

// HandleKey processes one key while the overlay is open. It returns the
// outcome and, for HistorySearchAccepted, the original prompt (with its
// original newlines) to load into the input without sending it.
func (v *HistorySearchView) HandleKey(msg tea.KeyPressMsg) (HistorySearchOutcome, string) {
	if !v.open {
		return HistorySearchUnhandledKey, ""
	}

	switch {
	case key.Matches(msg, historySearchKeys.cancel):
		v.Close()
		return HistorySearchClosed, ""

	case key.Matches(msg, historySearchKeys.accept):
		return v.acceptSelection()

	case key.Matches(msg, historySearchKeys.up):
		if len(v.matches) > 0 {
			v.selected = (v.selected + len(v.matches) - 1) % len(v.matches)
		}

	case key.Matches(msg, historySearchKeys.down):
		if len(v.matches) > 0 {
			v.selected = (v.selected + 1) % len(v.matches)
		}

	case key.Matches(msg, historySearchKeys.backspace):
		if len(v.query) > 0 {
			v.query = v.query[:len(v.query)-1]
			v.refilter()
		}

	default:
		if text := keys.PrintableText(msg); text != "" {
			v.query = append(v.query, []rune(text)...)
			v.refilter()
		}
	}

	return HistorySearchUnhandledKey, ""
}

// acceptSelection closes the overlay with the highlighted prompt as the result.
func (v *HistorySearchView) acceptSelection() (HistorySearchOutcome, string) {
	if len(v.matches) == 0 {
		return HistorySearchUnhandledKey, ""
	}
	entry := v.matches[v.selected].entry
	v.Close()
	return HistorySearchAccepted, entry
}

// refilter rebuilds the visible rows from the query, resetting the selection.
func (v *HistorySearchView) refilter() {
	v.selected = 0
	v.matches = make([]historySearchMatch, 0, len(v.entries))
	if len(v.query) == 0 {
		for i, entry := range v.entries {
			v.matches = append(v.matches, historySearchMatch{entry: entry, oneline: v.oneline[i]})
		}
		return
	}
	for _, m := range fuzzy.Find(string(v.query), v.oneline) {
		v.matches = append(v.matches, historySearchMatch{
			entry:   v.entries[m.Index],
			oneline: m.Str,
			indexes: m.MatchedIndexes,
		})
	}
}

// window returns the [start,end) slice of matches to render, scrolled to keep
// the selected row visible when more rows exist than fit.
func (v *HistorySearchView) window() (start, end int) {
	n := len(v.matches)
	if n <= maxVisibleHistoryRows {
		return 0, n
	}
	start = clampInt(v.selected-maxVisibleHistoryRows/2, 0, n-maxVisibleHistoryRows)
	return start, start + maxVisibleHistoryRows
}

// contentLines builds the unframed overlay lines; Render and GetHeight both
// derive from it so the reserved layout height never drifts from what's drawn.
func (v *HistorySearchView) contentLines() []string {
	if !v.open || v.styleProvider == nil {
		return nil
	}

	inner := max(v.width-4, 8)
	accent := v.styleProvider.GetThemeColor("accent")
	dim := v.styleProvider.GetThemeColor("dim")

	lines := []string{v.styleProvider.RenderWithColorAndBold(truncateRunes(v.headerText(), inner), accent)}
	lines = append(lines, v.queryLine(inner))

	start, end := v.window()
	if start > 0 {
		lines = append(lines, v.styleProvider.RenderWithColor(fmt.Sprintf("  ... (%d above)", start), dim))
	}
	for pos := start; pos < end; pos++ {
		lines = append(lines, v.renderRow(pos, accent, dim, inner))
	}
	if end < len(v.matches) {
		lines = append(lines, v.styleProvider.RenderWithColor(fmt.Sprintf("  ... (%d more)", len(v.matches)-end), dim))
	}
	lines = append(lines, v.styleProvider.RenderWithColor(truncateRunes(v.footerText(), inner), dim))
	return lines
}

func (v *HistorySearchView) headerText() string {
	return fmt.Sprintf("Prompt history (%d)", len(v.matches))
}

func (v *HistorySearchView) footerText() string {
	if len(v.matches) == 0 {
		return "no matches - esc to close"
	}
	return "type to filter - ↑/↓ select - enter load - esc cancel"
}

// queryLine renders the search prompt with an input-field style cursor.
func (v *HistorySearchView) queryLine(inner int) string {
	content := truncateRunes("Search: "+string(v.query), inner-1)
	return content + v.styleProvider.RenderCursor(" ")
}

// renderRow renders one matching prompt row, highlighting the fuzzy-matched
// characters and accenting the selected one.
func (v *HistorySearchView) renderRow(pos int, accent, dim string, inner int) string {
	m := v.matches[pos]
	text := truncateRunes(m.oneline, max(inner-2, 1))
	row := autocomplete.HighlightMatches(text, text, m.indexes)
	if pos == v.selected {
		return v.styleProvider.RenderWithColorAndBold("▶ "+row, accent)
	}
	return v.styleProvider.RenderWithColor("  "+row, dim)
}

// dedupeHistoryNewestFirst collapses duplicates to their most recent occurrence
// and returns the prompts newest-first.
func dedupeHistoryNewestFirst(history []string) []string {
	seen := make(map[string]struct{}, len(history))
	out := make([]string, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		entry := strings.TrimSpace(history[i])
		if entry == "" {
			continue
		}
		if _, dup := seen[entry]; dup {
			continue
		}
		seen[entry] = struct{}{}
		out = append(out, entry)
	}
	return out
}

// onelineHistoryEntry flattens a prompt onto one line for list display; the
// original text is what Enter loads.
func onelineHistoryEntry(entry string) string {
	return strings.ReplaceAll(entry, "\n", newlineDisplay)
}
