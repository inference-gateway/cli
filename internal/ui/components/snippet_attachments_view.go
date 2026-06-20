package components

import (
	"fmt"
	"strings"

	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// maxVisibleSnippetRows caps how many snippet child rows are shown at once; the
// window scrolls to keep the focused row visible. This keeps the inline box from
// pushing the chat input around when many snippets are attached.
const maxVisibleSnippetRows = 10

// SnippetAttachmentsView renders the pending snippet attachments as a small tree
// below the chat input: one parent row per file, one indented child row per
// captured line range. It is a passive renderer driven by ChatApplication
// (mirroring TodoBoxView) — focus, cursor and the snippet list are pushed in
// before each render. The selected content is sent with the next chat message.
type SnippetAttachmentsView struct {
	styleProvider *styles.Provider
	width         int
	snippets      []SnippetSelection
	order         []int
	focused       bool
	cursor        int
	focusHint     string
}

// NewSnippetAttachmentsView creates an empty attachments view.
func NewSnippetAttachmentsView(styleProvider *styles.Provider) *SnippetAttachmentsView {
	return &SnippetAttachmentsView{styleProvider: styleProvider, width: 80}
}

// SetWidth sets the component width.
func (v *SnippetAttachmentsView) SetWidth(width int) { v.width = width }

// SetFocusHint sets the key label shown in the unfocused header.
func (v *SnippetAttachmentsView) SetFocusHint(key string) { v.focusHint = key }

// SetData stores a copy of the pending selections and recomputes display order.
func (v *SnippetAttachmentsView) SetData(sels []SnippetSelection) {
	v.snippets = append(v.snippets[:0], sels...)
	v.order = groupedOrder(v.snippets)
	if v.cursor >= len(v.order) {
		v.cursor = max(len(v.order)-1, 0)
	}
}

// Count returns the number of attached snippets.
func (v *SnippetAttachmentsView) Count() int { return len(v.order) }

// IsFocused reports whether the tree currently has key focus.
func (v *SnippetAttachmentsView) IsFocused() bool { return v.focused }

// Blur removes key focus from the tree.
func (v *SnippetAttachmentsView) Blur() { v.focused = false }

// Focus gives the tree key focus, clamping the cursor into range.
func (v *SnippetAttachmentsView) Focus() {
	v.focused = true
	v.cursor = clampInt(v.cursor, 0, max(len(v.order)-1, 0))
}

// MoveCursor moves the selection by delta rows, clamping to the list bounds.
func (v *SnippetAttachmentsView) MoveCursor(delta int) {
	if len(v.order) == 0 {
		return
	}
	v.cursor = clampInt(v.cursor+delta, 0, len(v.order)-1)
}

// SelectedIndex returns the index into the app's pending list for the focused
// row, or -1 when empty.
func (v *SnippetAttachmentsView) SelectedIndex() int {
	if v.cursor < 0 || v.cursor >= len(v.order) {
		return -1
	}
	return v.order[v.cursor]
}

// GetHeight returns the rendered line count (0 when there is nothing to show).
func (v *SnippetAttachmentsView) GetHeight() int {
	lines := v.contentLines()
	if len(lines) == 0 {
		return 0
	}
	return len(lines) + 2
}

// Render returns the framed tree, or "" when there are no attachments.
func (v *SnippetAttachmentsView) Render() string {
	lines := v.contentLines()
	if len(lines) == 0 {
		return ""
	}
	return v.styleProvider.RenderBorderedBox(strings.Join(lines, "\n"), v.styleProvider.GetThemeColor("dim"), 0, 1)
}

// contentLines builds the (unframed) rendered lines. Render and GetHeight both
// derive from this so the reserved layout height never drifts from what's drawn.
func (v *SnippetAttachmentsView) contentLines() []string {
	if len(v.order) == 0 {
		return nil
	}
	accent := v.styleProvider.GetThemeColor("accent")
	dim := v.styleProvider.GetThemeColor("dim")
	inner := max(v.width-4, 8)

	lines := []string{v.styleProvider.RenderWithColorAndBold(truncateRunes(v.headerText(), inner), accent)}

	start, end := v.window()
	if start > 0 {
		lines = append(lines, v.styleProvider.RenderWithColor(fmt.Sprintf("  … (%d above)", start), dim))
	}
	lastFile := ""
	for pos := start; pos < end; pos++ {
		s := v.snippets[v.order[pos]]
		if s.File != lastFile {
			lines = append(lines, v.styleProvider.RenderWithColor("▸ "+truncatePathLeft(s.File, inner-2), dim))
			lastFile = s.File
		}
		lines = append(lines, v.renderChildRow(pos, s, accent, dim, inner))
	}
	if end < len(v.order) {
		lines = append(lines, v.styleProvider.RenderWithColor(fmt.Sprintf("    … (%d more)", len(v.order)-end), dim))
	}
	return lines
}

// renderChildRow renders one line-range row, highlighting it when it is the
// focused cursor row.
func (v *SnippetAttachmentsView) renderChildRow(pos int, s SnippetSelection, accent, dim string, inner int) string {
	label := truncateRunes("    "+lineRangeLabel(s), inner)
	if v.focused && pos == v.cursor {
		return v.styleProvider.RenderWithColorAndBold(label, accent)
	}
	return v.styleProvider.RenderWithColor(label, dim)
}

// headerText returns the box title; it shows the action keys when focused and a
// focus affordance otherwise.
func (v *SnippetAttachmentsView) headerText() string {
	count := len(v.order)
	if v.focused {
		return fmt.Sprintf("Attached context (%d) — ↑/↓ move · d remove · c clear · esc done", count)
	}
	if v.focusHint != "" {
		return fmt.Sprintf("Attached context (%d) · %s to edit · sent with your next message", count, v.focusHint)
	}
	return fmt.Sprintf("Attached context (%d) · sent with your next message", count)
}

// window returns the [start,end) slice of order to render, scrolled to keep the
// focused row visible when there are more rows than fit.
func (v *SnippetAttachmentsView) window() (start, end int) {
	n := len(v.order)
	if n <= maxVisibleSnippetRows {
		return 0, n
	}
	start = clampInt(v.cursor-maxVisibleSnippetRows/2, 0, n-maxVisibleSnippetRows)
	return start, start + maxVisibleSnippetRows
}

// groupedOrder returns indices into sels grouped by file in first-seen order, so
// the same file's ranges render together as one tree branch.
func groupedOrder(sels []SnippetSelection) []int {
	order := make([]int, 0, len(sels))
	seen := make(map[string]bool, len(sels))
	for i := range sels {
		if seen[sels[i].File] {
			continue
		}
		seen[sels[i].File] = true
		for j := range sels {
			if sels[j].File == sels[i].File {
				order = append(order, j)
			}
		}
	}
	return order
}

// lineRangeLabel renders a selection's 1-indexed inclusive line range.
func lineRangeLabel(s SnippetSelection) string {
	if s.StartLine == s.EndLine {
		return fmt.Sprintf("L%d", s.StartLine)
	}
	return fmt.Sprintf("L%d-%d", s.StartLine, s.EndLine)
}

// truncatePathLeft left-truncates a path to maxWidth runes, preserving the tail
// (filename) which is the most informative part.
func truncatePathLeft(p string, maxWidth int) string {
	r := []rune(p)
	if maxWidth <= 1 || len(r) <= maxWidth {
		return p
	}
	return "…" + string(r[len(r)-(maxWidth-1):])
}
