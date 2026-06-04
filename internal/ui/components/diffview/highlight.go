package diffview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// selectLexer picks a chroma lexer for the given path/content, falling back to
// content analysis and finally the plaintext lexer, then coalesces runs of the
// same token type. Used by the single-file Highlight helper (the in-line diff
// highlighter has its own cached, two-path variant in DiffView.lexer).
func selectLexer(path, content string) chroma.Lexer {
	l := lexers.Match(path)
	if l == nil {
		l = lexers.Analyse(content)
	}
	if l == nil {
		l = lexers.Fallback
	}
	return chroma.Coalesce(l)
}

// Highlight renders a whole file's contents as ANSI-styled, optionally
// line-numbered text for a preview pane (NOT a diff). It reuses the same chroma
// formatter as the in-line diff highlighter, so colors stay consistent. A nil
// style disables colorization; any tokenise/format error falls back to the
// plain (uncolored) content, so it never returns an error string.
func Highlight(path, content string, style *chroma.Style, lineNumbers bool) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")

	lines, ok := highlightToLines(path, content, style)
	if !ok {
		lines = strings.Split(content, "\n")
	}
	if !lineNumbers {
		return strings.Join(lines, "\n")
	}

	digits := len(strconv.Itoa(len(lines)))
	var b strings.Builder
	for i, ln := range lines {
		fmt.Fprintf(&b, "%*d │ %s\n", digits, i+1, ln)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// highlightToLines tokenises the whole file once (so multi-line strings and
// comments lex correctly), splits the token stream into lines, and formats each
// line independently into an ANSI-styled string. Splitting into lines first is
// required because chromaFormatter trims trailing newlines per token; feeding it
// the whole file would drop the line breaks. Returns ok=false when highlighting
// is unavailable (nil style, or a lexer/format error) so the caller falls back.
func highlightToLines(path, content string, style *chroma.Style) ([]string, bool) {
	if style == nil {
		return nil, false
	}
	it, err := selectLexer(path, content).Tokenise(nil, content)
	if err != nil {
		return nil, false
	}
	f := chromaFormatter("")
	tokenLines := chroma.SplitTokensIntoLines(it.Tokens())
	out := make([]string, 0, len(tokenLines))
	for _, lineTokens := range tokenLines {
		var sb strings.Builder
		if err := f.Format(&sb, style, chroma.Literator(lineTokens...)); err != nil {
			return nil, false
		}
		out = append(out, sb.String())
	}
	return out, true
}
