package components

import (
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// stripANSI removes ANSI escape sequences so tests can assert against the
// rendered text content without coupling to color codes.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' && s[i] != 'K' && s[i] != 'H' && s[i] != 'J' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestDiffRenderer_RenderDiff(t *testing.T) {
	themeService := domain.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)
	renderer := NewDiffRenderer(styleProvider)

	t.Run("New file creation", func(t *testing.T) {
		out := stripANSI(renderer.RenderDiff(DiffInfo{
			FilePath:   "test.go",
			OldContent: "",
			NewContent: "package main\n\nfunc main() {}\n",
			Title:      "Test Diff",
		}))
		if !strings.Contains(out, "@@") {
			t.Fatalf("expected hunk header in output:\n%s", out)
		}
		if !strings.Contains(out, "package main") {
			t.Fatalf("expected added content in output:\n%s", out)
		}
	})

	t.Run("File deletion", func(t *testing.T) {
		out := stripANSI(renderer.RenderDiff(DiffInfo{
			FilePath:   "test.go",
			OldContent: "package main\n\nfunc main() {}\n",
			NewContent: "",
			Title:      "Test Diff",
		}))
		if !strings.Contains(out, "@@") {
			t.Fatalf("expected hunk header in output:\n%s", out)
		}
		if !strings.Contains(out, "package main") {
			t.Fatalf("expected removed content visible in output:\n%s", out)
		}
	})

	t.Run("File modification", func(t *testing.T) {
		out := stripANSI(renderer.RenderDiff(DiffInfo{
			FilePath:   "test.go",
			OldContent: "Hello World\n",
			NewContent: "Hello Universe\n",
			Title:      "Test Diff",
		}))
		if !strings.Contains(out, "@@") {
			t.Fatalf("expected hunk header in output:\n%s", out)
		}
		if !strings.Contains(out, "Hello World") {
			t.Fatalf("expected old content in output:\n%s", out)
		}
		if !strings.Contains(out, "Hello Universe") {
			t.Fatalf("expected new content in output:\n%s", out)
		}
	})

	t.Run("No changes", func(t *testing.T) {
		out := stripANSI(renderer.RenderDiff(DiffInfo{
			FilePath:   "test.go",
			OldContent: "Same content\n",
			NewContent: "Same content\n",
			Title:      "Test Diff",
		}))
		if !strings.Contains(out, "test.go") {
			t.Fatalf("expected file path in output:\n%s", out)
		}
		// no hunks should be present for identical input
		if strings.Contains(out, "@@") {
			t.Fatalf("did not expect hunks for identical content:\n%s", out)
		}
	})
}

// TestDiffRenderer_MidFileInsertRegression guards against the parallel-array
// algorithm bug: the old renderer marked every line after a mid-file insert
// as both add+delete.
func TestDiffRenderer_MidFileInsertRegression(t *testing.T) {
	themeService := domain.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)
	renderer := NewDiffRenderer(styleProvider)

	before := "alpha\nbeta\ngamma\ndelta\nepsilon\n"
	after := "alpha\nbeta\nINSERTED\ngamma\ndelta\nepsilon\n"
	out := stripANSI(renderer.RenderDiff(DiffInfo{
		FilePath:   "f.txt",
		OldContent: before,
		NewContent: after,
	}))
	for _, tail := range []string{"gamma", "delta", "epsilon"} {
		if strings.Contains(out, "- "+tail) {
			t.Fatalf("line %q should not be marked deleted; cascade regression:\n%s", tail, out)
		}
	}
	if !strings.Contains(out, "INSERTED") {
		t.Fatalf("expected INSERTED in output:\n%s", out)
	}
}

// TestDiffRenderer_SetContextLines verifies the opt-in context-line override:
// the default keeps diffview's 3 lines of context around a change, while
// SetContextLines(2) trims to the 2 nearest unchanged lines on each side.
func TestDiffRenderer_SetContextLines(t *testing.T) {
	themeService := domain.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)

	before := "alpha\nbravo\ncharlie\ndelta\necho_OLD\nfoxtrot\ngolf\nhotel\nindia\n"
	after := "alpha\nbravo\ncharlie\ndelta\necho_NEW\nfoxtrot\ngolf\nhotel\nindia\n"
	info := DiffInfo{FilePath: "f.txt", OldContent: before, NewContent: after}

	def := stripANSI(NewDiffRenderer(styleProvider).RenderDiff(info))
	for _, want := range []string{"bravo", "hotel", "echo_NEW"} {
		if !strings.Contains(def, want) {
			t.Fatalf("default render should keep 3 context lines incl %q:\n%s", want, def)
		}
	}

	r := NewDiffRenderer(styleProvider)
	if got := r.SetContextLines(2); got != r {
		t.Fatalf("SetContextLines should return the same renderer for chaining")
	}
	ctx2 := stripANSI(r.RenderDiff(info))
	for _, want := range []string{"charlie", "delta", "foxtrot", "golf", "echo_NEW"} {
		if !strings.Contains(ctx2, want) {
			t.Fatalf("2-context render should still contain %q:\n%s", want, ctx2)
		}
	}
	for _, absent := range []string{"bravo", "hotel"} {
		if strings.Contains(ctx2, absent) {
			t.Fatalf("2-context render should trim the 3rd context line %q:\n%s", absent, ctx2)
		}
	}
}

func TestDiffRenderer_RenderMultiEditToolArguments(t *testing.T) {
	themeService := domain.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)
	renderer := NewDiffRenderer(styleProvider)

	t.Run("Multiple edits", func(t *testing.T) {
		args := map[string]any{
			"file_path": "/path/to/test.go",
			"edits": []any{
				map[string]any{
					"old_string": "Hello",
					"new_string": "Hi",
				},
				map[string]any{
					"old_string":  "World",
					"new_string":  "Universe",
					"replace_all": true,
				},
			},
		}
		out := stripANSI(renderer.RenderMultiEditToolArguments(args))
		if !strings.Contains(out, "test.go") {
			t.Fatalf("expected file name in output: %s", out)
		}
		if !strings.Contains(out, "2 edits") {
			t.Fatalf("expected '2 edits' in output: %s", out)
		}
		if !strings.Contains(out, "Edit 1:") || !strings.Contains(out, "Edit 2:") {
			t.Fatalf("expected per-edit headers in output: %s", out)
		}
		if !strings.Contains(out, "Replace all") {
			t.Fatalf("expected 'Replace all' for the second edit: %s", out)
		}
		if !strings.Contains(out, "Hi") || !strings.Contains(out, "Universe") {
			t.Fatalf("expected per-edit diff content in output: %s", out)
		}
	})

	t.Run("Empty edits array", func(t *testing.T) {
		out := stripANSI(renderer.RenderMultiEditToolArguments(map[string]any{
			"file_path": "/path/to/test.go",
			"edits":     []any{},
		}))
		if !strings.Contains(out, "test.go") || !strings.Contains(out, "0 edits") {
			t.Fatalf("expected file name and '0 edits' in output: %s", out)
		}
	})

	t.Run("Invalid edits format", func(t *testing.T) {
		out := stripANSI(renderer.RenderMultiEditToolArguments(map[string]any{
			"file_path": "/path/to/test.go",
			"edits":     "invalid",
		}))
		if !strings.Contains(out, "Invalid edits format") {
			t.Fatalf("expected error message for invalid format: %s", out)
		}
	})
}

// TestDiffRenderer_WriteNewFilePreviewCap covers the new-file preview line cap:
// the default caps at 50 with a "more lines" footer, a trailing newline does not
// produce a phantom extra line, and SetMaxLines(-1) renders every line uncapped.
func TestDiffRenderer_WriteNewFilePreviewCap(t *testing.T) {
	styleProvider := styles.NewProvider(domain.NewThemeProvider())

	var b strings.Builder
	for i := 1; i <= 50; i++ {
		b.WriteString("line\n")
	}
	args := map[string]any{"file_path": "/no/such/file_probe.txt", "content": b.String()}

	capped := stripANSI(NewDiffRenderer(styleProvider).RenderWriteToolArguments(args))
	if strings.Contains(capped, "more lines") {
		t.Errorf("50 lines + trailing newline should not be truncated, got a footer:\n%s", capped)
	}
	if !strings.Contains(capped, "50 │") {
		t.Errorf("expected line 50 to render, got:\n%s", capped)
	}

	b.Reset()
	for i := 1; i <= 60; i++ {
		b.WriteString("line\n")
	}
	args["content"] = b.String()
	over := stripANSI(NewDiffRenderer(styleProvider).RenderWriteToolArguments(args))
	if !strings.Contains(over, "more lines") {
		t.Errorf("60 lines should be capped with a footer, got:\n%s", over)
	}

	full := stripANSI(NewDiffRenderer(styleProvider).SetMaxLines(-1).RenderWriteToolArguments(args))
	if strings.Contains(full, "more lines") {
		t.Errorf("SetMaxLines(-1) should render uncapped, got a footer:\n%s", full)
	}
	if !strings.Contains(full, "60 │") {
		t.Errorf("expected line 60 to render uncapped, got:\n%s", full)
	}
}

func TestDiffRenderer_Construction(t *testing.T) {
	themeService := domain.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)

	if NewDiffRenderer(styleProvider) == nil {
		t.Fatal("NewDiffRenderer returned nil")
	}
	if NewToolDiffRenderer(styleProvider) == nil {
		t.Fatal("NewToolDiffRenderer returned nil")
	}

	out := NewToolDiffRenderer(styleProvider).RenderDiff(DiffInfo{
		FilePath:   "test.go",
		OldContent: "old\n",
		NewContent: "new\n",
		Title:      "Test",
	})
	if out == "" {
		t.Fatal("expected non-empty output")
	}
}
