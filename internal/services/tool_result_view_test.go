package services

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTreeContinuationIndent(t *testing.T) {
	cases := map[string]string{
		"├─ ":    "│  ",
		"└─ ":    "   ",
		"│  ├─ ": "│  │  ",
		"│  └─ ": "│     ",
		"":       "",
	}
	for prefix, want := range cases {
		if got := treeContinuationIndent(prefix); got != want {
			t.Errorf("treeContinuationIndent(%q) = %q, want %q", prefix, got, want)
		}
	}
}

func TestWrapTreeLine_shortLineUnchanged(t *testing.T) {
	line := "├─ Status: ✓ Success"
	got := wrapTreeLine(line, 80)
	if len(got) != 1 || got[0] != line {
		t.Errorf("short line should be unchanged, got %#v", got)
	}
}

func TestWrapTreeLine_wrapsAndPreservesContent(t *testing.T) {
	const text = "Write requires approval, but approvals are not available in this session and the action was NOT executed."
	line := "├─ Error: " + text
	width := 40

	got := wrapTreeLine(line, width)
	if len(got) < 2 {
		t.Fatalf("expected the long line to wrap into multiple lines, got %#v", got)
	}

	if !strings.HasPrefix(got[0], "├─ Error: ") {
		t.Errorf("first wrapped line should keep the field prefix, got %q", got[0])
	}
	for i, ln := range got {
		if utf8.RuneCountInString(ln) > width {
			t.Errorf("wrapped line %d exceeds width %d: %q (%d)", i, width, ln, utf8.RuneCountInString(ln))
		}
		if i > 0 && !strings.HasPrefix(ln, "│  ") {
			t.Errorf("continuation line %d should hang-indent under the field, got %q", i, ln)
		}
	}

	words := strings.Fields(strings.TrimPrefix(got[0], "├─ Error: "))
	for _, ln := range got[1:] {
		_, rest := splitTreePrefix(ln)
		words = append(words, strings.Fields(rest)...)
	}
	if rejoined := strings.Join(words, " "); rejoined != text {
		t.Errorf("wrapped content lost text:\n got:  %q\n want: %q", rejoined, text)
	}
}

func TestWrapTreeLine_avoidsSliverColumns(t *testing.T) {
	line := "│  │  │  ├─ command: this is a fairly long bash command that would wrap"
	got := wrapTreeLine(line, 14)
	if len(got) != 1 {
		t.Errorf("expected no wrap when the content column is below the minimum, got %#v", got)
	}
}

func TestWrapTreeLines_endToEnd(t *testing.T) {
	tree := strings.Join([]string{
		"Bash(command=...)",
		"├─ Status: ✗ Failed",
		"├─ Arguments:",
		"│  └─ command: " + strings.Repeat("echo hello && ", 12) + "done",
	}, "\n")

	wrapped := wrapTreeLines(tree, 50)
	for _, ln := range strings.Split(wrapped, "\n") {
		if utf8.RuneCountInString(ln) > 50 {
			t.Errorf("line exceeds width: %q (%d)", ln, utf8.RuneCountInString(ln))
		}
	}
	if !strings.Contains(wrapped, "done") {
		t.Error("the tail of a long command should survive wrapping (was clipped)")
	}
}
