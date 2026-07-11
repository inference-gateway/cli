// Package diffview renders unified or side-by-side file diffs to a string of
// ANSI-styled output, using go-udiff for the underlying diff algorithm and
// chroma for in-line syntax highlighting.
//
// The structure (file/style/split/builder) mirrors charmbracelet/crush's
// internal/ui/diffview package, adapted to lipgloss v1 and the existing infer
// CLI styling abstractions.
//
// Typical use:
//
//	out := diffview.New().
//	    Before(path, oldContent).
//	    After(path, newContent).
//	    FileName(path).
//	    Width(termWidth).
//	    Style(diffview.DefaultDarkStyle()).
//	    String()
package diffview

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/aymanbagabas/go-udiff"
	"github.com/zeebo/xxh3"
)

// Layout controls whether the diff is rendered as a single column (Unified) or
// side-by-side (Split). Auto picks Split when the available width is >=
// SplitThreshold, else Unified.
type Layout int

const (
	LayoutAuto Layout = iota
	LayoutUnified
	LayoutSplit
)

const (
	defaultContextLines  = 3
	defaultTabWidth      = 4
	defaultSplitMinWidth = 160
	leadingSymbolsSize   = 2
	lineNumPadding       = 1
)

type file struct {
	path    string
	content string
}

// DiffView is the fluent builder + renderer.
type DiffView struct {
	layout        Layout
	splitMinWidth int
	before        file
	after         file
	fileName      string
	contextLines  int
	lineNumbers   bool
	width         int
	tabWidth      int
	style         Style
	chromaStyle   *chroma.Style

	// computed lazily on String()
	isComputed      bool
	err             error
	unified         udiff.UnifiedDiff
	splitHunks      []splitHunk
	beforeNumDigits int
	afterNumDigits  int
	codeWidth       int
	fullCodeWidth   int
	extraColOnAfter bool
	resolvedLayout  Layout

	cachedLexer chroma.Lexer
	syntaxCache map[string]string
}

// New returns a DiffView with sensible defaults (unified layout, 3 context
// lines, 4-space tab expansion, line numbers on, dark style).
func New() *DiffView {
	return &DiffView{
		layout:        LayoutAuto,
		splitMinWidth: defaultSplitMinWidth,
		contextLines:  defaultContextLines,
		lineNumbers:   true,
		tabWidth:      defaultTabWidth,
		style:         DefaultDarkStyle(),
		syntaxCache:   make(map[string]string),
	}
}

// Before sets the "before" file (path is used for chroma lexer selection).
func (dv *DiffView) Before(path, content string) *DiffView {
	dv.before = file{path: path, content: content}
	dv.clearCaches()
	return dv
}

// After sets the "after" file.
func (dv *DiffView) After(path, content string) *DiffView {
	dv.after = file{path: path, content: content}
	dv.clearCaches()
	return dv
}

// FileName sets the file header rendered above the first hunk. Empty disables it.
func (dv *DiffView) FileName(name string) *DiffView {
	dv.fileName = name
	return dv
}

// Width sets the total render width. Pass 0 (or omit) to render at natural width.
func (dv *DiffView) Width(w int) *DiffView { dv.width = w; return dv }

// Layout overrides the layout selection. Defaults to Auto.
func (dv *DiffView) Layout(l Layout) *DiffView { dv.layout = l; return dv }

// Style overrides the visual style. Defaults to DefaultDarkStyle.
func (dv *DiffView) Style(s Style) *DiffView { dv.style = s; return dv }

// WithChromaStyle enables in-line syntax highlighting for diff content
// using the given chroma style. Pass nil (or omit the call) for no highlighting.
func (dv *DiffView) WithChromaStyle(s *chroma.Style) *DiffView {
	dv.chromaStyle = s
	dv.clearCaches()
	return dv
}

// ContextLines sets the number of unchanged context lines around each hunk.
func (dv *DiffView) ContextLines(n int) *DiffView { dv.contextLines = n; return dv }

// String renders the diff.
func (dv *DiffView) String() string {
	dv.normalizeLineEndings()
	dv.replaceTabs()
	if err := dv.computeDiff(); err != nil {
		return err.Error()
	}
	dv.resolveLayout()
	if dv.resolvedLayout == LayoutSplit {
		dv.convertDiffToSplit()
	}
	dv.adjustStyles()
	dv.detectNumDigits()
	if dv.width <= 0 {
		dv.detectCodeWidth()
	} else {
		dv.resizeCodeWidth()
	}

	var out string
	switch dv.resolvedLayout {
	case LayoutUnified:
		out = dv.renderUnified()
	case LayoutSplit:
		out = dv.renderSplit()
	}
	return strings.TrimSuffix(out, "\n")
}

// --- internals ---

func (dv *DiffView) clearCaches() {
	dv.cachedLexer = nil
	dv.clearSyntaxCache()
	dv.isComputed = false
}

func (dv *DiffView) clearSyntaxCache() {
	for k := range dv.syntaxCache {
		delete(dv.syntaxCache, k)
	}
}

func (dv *DiffView) normalizeLineEndings() {
	dv.before.content = strings.ReplaceAll(dv.before.content, "\r\n", "\n")
	dv.after.content = strings.ReplaceAll(dv.after.content, "\r\n", "\n")
}

func (dv *DiffView) replaceTabs() {
	spaces := strings.Repeat(" ", dv.tabWidth)
	dv.before.content = strings.ReplaceAll(dv.before.content, "\t", spaces)
	dv.after.content = strings.ReplaceAll(dv.after.content, "\t", spaces)
}

func (dv *DiffView) computeDiff() error {
	if dv.isComputed {
		return dv.err
	}
	dv.isComputed = true
	// Lines is the line-level edit script which produces clean line-shift
	// hunks; Strings would be rune-level and visually misleads on text diffs.
	edits := udiff.Lines(dv.before.content, dv.after.content)
	dv.unified, dv.err = udiff.ToUnifiedDiff(
		dv.before.path, dv.after.path,
		dv.before.content, edits,
		dv.contextLines,
	)
	return dv.err
}

func (dv *DiffView) resolveLayout() {
	switch dv.layout {
	case LayoutUnified, LayoutSplit:
		dv.resolvedLayout = dv.layout
	default: // Auto
		if dv.width >= dv.splitMinWidth {
			dv.resolvedLayout = LayoutSplit
		} else {
			dv.resolvedLayout = LayoutUnified
		}
	}
}

func (dv *DiffView) convertDiffToSplit() {
	dv.splitHunks = make([]splitHunk, len(dv.unified.Hunks))
	for i, h := range dv.unified.Hunks {
		dv.splitHunks[i] = hunkToSplit(h)
	}
}

func (dv *DiffView) adjustStyles() {
	pad := func(s lipgloss.Style) lipgloss.Style {
		return s.Padding(0, lineNumPadding).Align(lipgloss.Right)
	}
	dv.style.MissingLine.LineNumber = pad(dv.style.MissingLine.LineNumber)
	dv.style.DividerLine.LineNumber = pad(dv.style.DividerLine.LineNumber)
	dv.style.EqualLine.LineNumber = pad(dv.style.EqualLine.LineNumber)
	dv.style.InsertLine.LineNumber = pad(dv.style.InsertLine.LineNumber)
	dv.style.DeleteLine.LineNumber = pad(dv.style.DeleteLine.LineNumber)
	dv.style.Filename.LineNumber = pad(dv.style.Filename.LineNumber)
}

func (dv *DiffView) detectNumDigits() {
	dv.beforeNumDigits = 1
	dv.afterNumDigits = 1
	for _, h := range dv.unified.Hunks {
		dv.beforeNumDigits = max(dv.beforeNumDigits, len(strconv.Itoa(h.FromLine+len(h.Lines))))
		dv.afterNumDigits = max(dv.afterNumDigits, len(strconv.Itoa(h.ToLine+len(h.Lines))))
	}
}

func (dv *DiffView) detectCodeWidth() {
	dv.codeWidth = 0
	switch dv.resolvedLayout {
	case LayoutUnified:
		for _, h := range dv.unified.Hunks {
			shown := lipgloss.Width(dv.hunkHeader(h))
			for _, l := range h.Lines {
				w := lipgloss.Width(strings.TrimSuffix(l.Content, "\n")) + 1
				if w > dv.codeWidth {
					dv.codeWidth = w
				}
				if shown > dv.codeWidth {
					dv.codeWidth = shown
				}
			}
		}
	case LayoutSplit:
		for i, h := range dv.splitHunks {
			shown := lipgloss.Width(dv.hunkHeader(dv.unified.Hunks[i]))
			for _, l := range h.lines {
				if l.before != nil {
					w := lipgloss.Width(strings.TrimSuffix(l.before.Content, "\n")) + 1
					if w > dv.codeWidth {
						dv.codeWidth = w
					}
				}
				if l.after != nil {
					w := lipgloss.Width(strings.TrimSuffix(l.after.Content, "\n")) + 1
					if w > dv.codeWidth {
						dv.codeWidth = w
					}
				}
				if shown > dv.codeWidth {
					dv.codeWidth = shown
				}
			}
		}
	}
	dv.fullCodeWidth = dv.codeWidth + leadingSymbolsSize
}

func (dv *DiffView) resizeCodeWidth() {
	gutter := dv.beforeNumDigits + dv.afterNumDigits + lineNumPadding*4
	switch dv.resolvedLayout {
	case LayoutUnified:
		dv.codeWidth = dv.width - gutter - leadingSymbolsSize
	case LayoutSplit:
		remaining := dv.width - gutter - leadingSymbolsSize*2
		dv.codeWidth = remaining / 2
		dv.extraColOnAfter = remaining%2 != 0
	}
	if dv.codeWidth < 1 {
		dv.codeWidth = 1
	}
	dv.fullCodeWidth = dv.codeWidth + leadingSymbolsSize
}

func (dv *DiffView) hunkHeader(h *udiff.Hunk) string {
	bShown, aShown := 0, 0
	for _, l := range h.Lines {
		switch l.Kind {
		case udiff.Equal:
			bShown++
			aShown++
		case udiff.Insert:
			aShown++
		case udiff.Delete:
			bShown++
		}
	}
	return fmt.Sprintf("  @@ -%d,%d +%d,%d @@ ", h.FromLine, bShown, h.ToLine, aShown)
}

func (dv *DiffView) padNum(n any, digits int) string {
	switch v := n.(type) {
	case int:
		return fmt.Sprintf("%*d", digits, v)
	case string:
		return fmt.Sprintf("%*s", digits, v)
	}
	return ""
}

// renderUnified produces the unified-format string.
func (dv *DiffView) renderUnified() string {
	var b strings.Builder
	containerStyle := lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth)

	writeFilename := func() {
		if dv.fileName == "" {
			return
		}
		ls := dv.style.Filename
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum("…", dv.beforeNumDigits)))
			b.WriteString(ls.LineNumber.Render(dv.padNum("…", dv.afterNumDigits)))
		}
		b.WriteString(ls.Code.Width(dv.fullCodeWidth).Render("  " + dv.fileName))
		b.WriteByte('\n')
	}

	for i, h := range dv.unified.Hunks {
		if i == 0 {
			writeFilename()
		}
		ls := dv.style.DividerLine
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum("…", dv.beforeNumDigits)))
			b.WriteString(ls.LineNumber.Render(dv.padNum("…", dv.afterNumDigits)))
		}
		b.WriteString(ls.Code.Width(dv.fullCodeWidth).Render(dv.hunkHeader(h)))
		b.WriteByte('\n')

		beforeLine := h.FromLine
		afterLine := h.ToLine
		for _, l := range h.Lines {
			switch l.Kind {
			case udiff.Equal:
				ls := dv.style.EqualLine
				code, _ := dv.lineContent(l.Content, ls)
				if dv.lineNumbers {
					b.WriteString(ls.LineNumber.Render(dv.padNum(beforeLine, dv.beforeNumDigits)))
					b.WriteString(ls.LineNumber.Render(dv.padNum(afterLine, dv.afterNumDigits)))
				}
				b.WriteString(containerStyle.Render(ls.Code.Width(dv.fullCodeWidth).Render("  " + code)))
				beforeLine++
				afterLine++
			case udiff.Insert:
				ls := dv.style.InsertLine
				code, _ := dv.lineContent(l.Content, ls)
				if dv.lineNumbers {
					b.WriteString(ls.LineNumber.Render(dv.padNum(" ", dv.beforeNumDigits)))
					b.WriteString(ls.LineNumber.Render(dv.padNum(afterLine, dv.afterNumDigits)))
				}
				b.WriteString(containerStyle.Render(
					ls.Symbol.Render("+ ") +
						ls.Code.Width(dv.codeWidth).Render(code),
				))
				afterLine++
			case udiff.Delete:
				ls := dv.style.DeleteLine
				code, _ := dv.lineContent(l.Content, ls)
				if dv.lineNumbers {
					b.WriteString(ls.LineNumber.Render(dv.padNum(beforeLine, dv.beforeNumDigits)))
					b.WriteString(ls.LineNumber.Render(dv.padNum(" ", dv.afterNumDigits)))
				}
				b.WriteString(containerStyle.Render(
					ls.Symbol.Render("- ") +
						ls.Code.Width(dv.codeWidth).Render(code),
				))
				beforeLine++
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderSplit produces the side-by-side string.
func (dv *DiffView) renderSplit() string {
	var b strings.Builder
	beforePanel := lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth)
	afterExtra := 0
	if dv.extraColOnAfter {
		afterExtra = 1
	}
	afterPanel := lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth + afterExtra)

	writeFilename := func() {
		if dv.fileName == "" {
			return
		}
		ls := dv.style.Filename
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum("…", dv.beforeNumDigits)))
		}
		b.WriteString(ls.Code.Width(dv.fullCodeWidth).Render("  " + dv.fileName))
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum("…", dv.afterNumDigits)))
		}
		b.WriteString(ls.Code.Width(dv.fullCodeWidth + afterExtra).Render(" "))
		b.WriteByte('\n')
	}

	for i, h := range dv.splitHunks {
		if i == 0 {
			writeFilename()
		}
		ls := dv.style.DividerLine
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum("…", dv.beforeNumDigits)))
		}
		b.WriteString(ls.Code.Width(dv.fullCodeWidth).Render(dv.hunkHeader(dv.unified.Hunks[i])))
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum("…", dv.afterNumDigits)))
		}
		b.WriteString(ls.Code.Width(dv.fullCodeWidth + afterExtra).Render(" "))
		b.WriteByte('\n')

		beforeLine := h.fromLine
		afterLine := h.toLine
		for _, l := range h.lines {
			dv.renderSplitSide(&b, l.before, true, &beforeLine, beforePanel)
			dv.renderSplitSide(&b, l.after, false, &afterLine, afterPanel)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (dv *DiffView) renderSplitSide(b *strings.Builder, l *udiff.Line, isBefore bool, lineNo *int, panel lipgloss.Style) {
	digits := dv.beforeNumDigits
	width := dv.fullCodeWidth
	if !isBefore {
		digits = dv.afterNumDigits
		if dv.extraColOnAfter {
			width = dv.fullCodeWidth + 1
		}
	}

	if l == nil {
		ls := dv.style.MissingLine
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum(" ", digits)))
		}
		b.WriteString(panel.Render(ls.Code.Width(width).Render("  ")))
		return
	}

	switch l.Kind {
	case udiff.Equal:
		ls := dv.style.EqualLine
		code, _ := dv.lineContent(l.Content, ls)
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum(*lineNo, digits)))
		}
		b.WriteString(panel.Render(ls.Code.Width(width).Render("  " + code)))
		*lineNo++
	case udiff.Insert:
		ls := dv.style.InsertLine
		code, _ := dv.lineContent(l.Content, ls)
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum(*lineNo, digits)))
		}
		codeW := dv.codeWidth
		if !isBefore && dv.extraColOnAfter {
			codeW++
		}
		b.WriteString(panel.Render(ls.Symbol.Render("+ ") + ls.Code.Width(codeW).Render(code)))
		*lineNo++
	case udiff.Delete:
		ls := dv.style.DeleteLine
		code, _ := dv.lineContent(l.Content, ls)
		if dv.lineNumbers {
			b.WriteString(ls.LineNumber.Render(dv.padNum(*lineNo, digits)))
		}
		b.WriteString(panel.Render(ls.Symbol.Render("- ") + ls.Code.Width(dv.codeWidth).Render(code)))
		*lineNo++
	}
}

func (dv *DiffView) lineContent(in string, ls LineStyle) (string, bool) {
	content := strings.TrimSuffix(in, "\n")
	// Pull the background color out of the lipgloss v2 style as RGB hex so
	// the chroma formatter can preserve it across tokens.
	bg := ""
	if c := ls.Code.GetBackground(); c != nil {
		r, g, bch, a := c.RGBA()
		if a > 0 {
			bg = fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(bch>>8))
		}
	}
	content = dv.highlightCode(content, bg)
	return content, false
}

func (dv *DiffView) highlightCode(source, bgHex string) string {
	if dv.chromaStyle == nil || source == "" {
		return source
	}
	key := dv.syntaxCacheKey(source, bgHex)
	if cached, ok := dv.syntaxCache[key]; ok {
		return cached
	}
	l := dv.lexer()
	f := chromaFormatter(bgHex)
	it, err := l.Tokenise(nil, source)
	if err != nil {
		return source
	}
	var out strings.Builder
	if err := f.Format(&out, dv.chromaStyle, it); err != nil {
		return source
	}
	res := out.String()
	dv.syntaxCache[key] = res
	return res
}

func (dv *DiffView) syntaxCacheKey(source, bg string) string {
	h := xxh3.New()
	_, _ = h.WriteString(source)
	_, _ = h.WriteString("|")
	_, _ = h.WriteString(bg)
	return strconv.FormatUint(h.Sum64(), 16)
}

func (dv *DiffView) lexer() chroma.Lexer {
	if dv.cachedLexer != nil {
		return dv.cachedLexer
	}
	l := lexers.Match(dv.before.path)
	if l == nil {
		l = lexers.Match(dv.after.path)
	}
	if l == nil {
		l = lexers.Analyse(dv.after.content)
	}
	if l == nil {
		l = lexers.Fallback
	}
	dv.cachedLexer = chroma.Coalesce(l)
	return dv.cachedLexer
}
