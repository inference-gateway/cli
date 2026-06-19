package components

import (
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

func newTestSnippetView() *SnippetAttachmentsView {
	ts := domain.NewThemeProvider()
	v := NewSnippetAttachmentsView(styles.NewProvider(ts))
	v.SetWidth(80)
	return v
}

func TestSnippetAttachments_EmptyRendersNothing(t *testing.T) {
	v := newTestSnippetView()
	if got := v.Render(); got != "" {
		t.Fatalf("empty Render() = %q, want \"\"", got)
	}
	if got := v.GetHeight(); got != 0 {
		t.Fatalf("empty GetHeight() = %d, want 0", got)
	}
	if got := v.SelectedIndex(); got != -1 {
		t.Fatalf("empty SelectedIndex() = %d, want -1", got)
	}
}

func TestSnippetAttachments_RendersFilesAndRanges(t *testing.T) {
	v := newTestSnippetView()
	v.SetData([]SnippetSelection{
		{File: "internal/app/chat.go", StartLine: 3, EndLine: 5},
		{File: "cmd/root.go", StartLine: 7, EndLine: 7},
	})
	out := v.Render()
	for _, want := range []string{"chat.go", "L3-5", "root.go", "L7"} {
		if !strings.Contains(out, want) {
			t.Errorf("Render() missing %q\n--- output ---\n%s", want, out)
		}
	}
	if v.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", v.Count())
	}
	if v.GetHeight() <= 0 {
		t.Fatalf("GetHeight() = %d, want > 0", v.GetHeight())
	}
}

func TestSnippetAttachments_GroupsByFileAndMapsIndex(t *testing.T) {
	v := newTestSnippetView()
	// App order interleaves files; the display groups them, but SelectedIndex
	// must map back to the original app-slice index for correct removal.
	v.SetData([]SnippetSelection{
		{File: "a.go", StartLine: 1, EndLine: 1}, // app idx 0
		{File: "b.go", StartLine: 2, EndLine: 2}, // app idx 1
		{File: "a.go", StartLine: 9, EndLine: 9}, // app idx 2
	})
	v.Focus()

	// Display order groups a.go's two ranges first (idx 0, 2), then b.go (idx 1).
	if got := v.SelectedIndex(); got != 0 {
		t.Fatalf("cursor 0 SelectedIndex = %d, want 0 (a.go:1)", got)
	}
	v.MoveCursor(1)
	if got := v.SelectedIndex(); got != 2 {
		t.Fatalf("cursor 1 SelectedIndex = %d, want 2 (a.go:9)", got)
	}
	v.MoveCursor(1)
	if got := v.SelectedIndex(); got != 1 {
		t.Fatalf("cursor 2 SelectedIndex = %d, want 1 (b.go:2)", got)
	}
	v.MoveCursor(5) // clamp at the end
	if got := v.SelectedIndex(); got != 1 {
		t.Fatalf("over-move SelectedIndex = %d, want 1 (clamped)", got)
	}
	v.MoveCursor(-99) // clamp at the start
	if got := v.SelectedIndex(); got != 0 {
		t.Fatalf("under-move SelectedIndex = %d, want 0 (clamped)", got)
	}
}

func TestSnippetAttachments_SetDataClampsCursor(t *testing.T) {
	v := newTestSnippetView()
	v.SetData([]SnippetSelection{
		{File: "a.go", StartLine: 1, EndLine: 1},
		{File: "a.go", StartLine: 2, EndLine: 2},
		{File: "a.go", StartLine: 3, EndLine: 3},
	})
	v.Focus()
	v.MoveCursor(2) // cursor → 2
	// Shrink the list; cursor must clamp back into range.
	v.SetData([]SnippetSelection{{File: "a.go", StartLine: 1, EndLine: 1}})
	if got := v.SelectedIndex(); got != 0 {
		t.Fatalf("after shrink SelectedIndex = %d, want 0", got)
	}
}

func TestSnippetAttachments_TruncatePathLeftKeepsTail(t *testing.T) {
	got := truncatePathLeft("internal/ui/components/snippet_attachments_view.go", 20)
	if !strings.HasSuffix(got, "view.go") {
		t.Fatalf("truncatePathLeft should keep the filename tail, got %q", got)
	}
	if []rune(got)[0] != '…' {
		t.Fatalf("truncatePathLeft should prefix an ellipsis, got %q", got)
	}
}
