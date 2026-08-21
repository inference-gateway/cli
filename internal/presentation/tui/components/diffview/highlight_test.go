package diffview

import (
	"strings"
	"testing"

	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

func TestHighlight_PlainPassthrough(t *testing.T) {
	out := Highlight("x.txt", "alpha\nbeta", nil, true)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "alpha") || !strings.Contains(lines[1], "beta") {
		t.Fatalf("content not preserved: %q", out)
	}
	if !strings.Contains(lines[0], "1") || !strings.Contains(lines[1], "2") {
		t.Fatalf("expected a line-number gutter: %q", out)
	}
}

func TestHighlight_NoLineNumbers(t *testing.T) {
	out := Highlight("x.txt", "one\ntwo", nil, false)
	if out != "one\ntwo" {
		t.Fatalf("want plain join, got %q", out)
	}
}

// TestHighlight_ExpandsTabs guards that tabs are expanded to spaces. A literal
// tab is measured as 0 cells by ansi.StringWidth but rendered as a tab stop by
// the terminal; left in place it overflows the fixed-width preview pane and
// corrupts the explorer layout. Both the plain fallback and the highlighted path
// must be tab-free.
func TestHighlight_ExpandsTabs(t *testing.T) {
	indent := strings.Repeat(" ", defaultTabWidth)

	plain := Highlight("x.go", "\tone\n\t\ttwo", nil, false)
	if strings.Contains(plain, "\t") {
		t.Fatalf("plain path left a literal tab: %q", plain)
	}
	if plain != indent+"one\n"+indent+indent+"two" {
		t.Fatalf("tabs not expanded to %d spaces: %q", defaultTabWidth, plain)
	}

	styled := Highlight("main.go", "\tx := 1", chromastyles.Get("github-dark"), false)
	if strings.Contains(styled, "\t") {
		t.Fatalf("highlighted path left a literal tab: %q", styled)
	}
}

// TestHighlight_MultilineConstructPreservesLineCount guards the whole-file
// tokenise-then-split approach: a multi-line raw string must not collapse or
// add lines (a per-line tokeniser would mis-handle it).
func TestHighlight_MultilineConstructPreservesLineCount(t *testing.T) {
	src := "package main\n\nvar s = `line1\nline2\nline3`\n\nfunc main() {}\n"
	style := chromastyles.Get("github-dark")

	out := Highlight("main.go", src, style, false)

	gotLines := strings.Count(out, "\n") + 1
	wantLines := strings.Count(strings.TrimSuffix(src, "\n"), "\n") + 1
	if gotLines != wantLines {
		t.Fatalf("styled line count = %d, want %d\nout=%q", gotLines, wantLines, out)
	}
}
