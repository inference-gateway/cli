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
