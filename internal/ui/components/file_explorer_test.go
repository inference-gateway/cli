package components

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

func newTestExplorer(t *testing.T, root string) *FileExplorerImpl {
	t.Helper()
	ts := domain.NewThemeProvider()
	e := NewFileExplorer(root, styles.NewProvider(ts), ts, config.KeybindingsConfig{})
	e.SetWidth(120)
	e.SetHeight(40)
	return e
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rowRels(e *FileExplorerImpl) []string {
	rels := make([]string, len(e.rows))
	for i, r := range e.rows {
		rels[i] = r.node.relPath
	}
	return rels
}

func explorerHasRow(e *FileExplorerImpl, rel string) bool {
	_, ok := e.indexOfRel(rel)
	return ok
}

func mustRowIndex(t *testing.T, e *FileExplorerImpl, rel string) int {
	t.Helper()
	i, ok := e.indexOfRel(rel)
	if !ok {
		t.Fatalf("row %q not found in %v", rel, rowRels(e))
	}
	return i
}

func TestExplorer_FlattenRootDirsFirst(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "b.txt"), "b")
	writeTestFile(t, filepath.Join(root, "z.txt"), "z")
	writeTestFile(t, filepath.Join(root, "alpha", "c.txt"), "c")

	e := newTestExplorer(t, root)

	want := []string{"alpha", "b.txt", "z.txt"}
	if got := rowRels(e); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v (dirs first, then files)", got, want)
	}
	if explorerHasRow(e, "alpha/c.txt") {
		t.Fatal("alpha/c.txt should be hidden while alpha is collapsed")
	}
}

func TestExplorer_ExpandLoadsChildrenLazily(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "alpha", "c.txt"), "c")

	e := newTestExplorer(t, root)
	if _, ok := e.children["alpha"]; ok {
		t.Fatal("alpha children should not be read before it is expanded")
	}

	e.cursor = mustRowIndex(t, e, "alpha")
	e.setExpanded(true)

	if _, ok := e.children["alpha"]; !ok {
		t.Fatal("alpha children should be cached after expand")
	}
	if !explorerHasRow(e, "alpha/c.txt") {
		t.Fatalf("alpha/c.txt should be visible after expand; rows=%v", rowRels(e))
	}
}

func TestExplorer_CollapseHidesChildren(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "alpha", "c.txt"), "c")

	e := newTestExplorer(t, root)
	e.cursor = mustRowIndex(t, e, "alpha")
	e.setExpanded(true)
	if !explorerHasRow(e, "alpha/c.txt") {
		t.Fatal("precondition: child visible after expand")
	}

	e.cursor = mustRowIndex(t, e, "alpha")
	e.setExpanded(false)
	if explorerHasRow(e, "alpha/c.txt") {
		t.Fatal("child should be hidden after collapse")
	}
}

func TestExplorer_GitignoreHonored(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".gitignore"), "ignored/\n*.log\n")
	writeTestFile(t, filepath.Join(root, "ignored", "x.txt"), "x")
	writeTestFile(t, filepath.Join(root, "keep.txt"), "k")
	writeTestFile(t, filepath.Join(root, "debug.log"), "l")

	e := newTestExplorer(t, root)
	rels := rowRels(e)

	if explorerHasRow(e, "ignored") {
		t.Errorf("gitignored dir should be hidden; rows=%v", rels)
	}
	if explorerHasRow(e, "debug.log") {
		t.Errorf("*.log file should be hidden; rows=%v", rels)
	}
	if !explorerHasRow(e, "keep.txt") {
		t.Errorf("keep.txt should be visible; rows=%v", rels)
	}
}

func TestExplorer_ShowHiddenToggle(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".env"), "secret")
	writeTestFile(t, filepath.Join(root, "visible.txt"), "v")

	e := newTestExplorer(t, root)
	if explorerHasRow(e, ".env") {
		t.Fatal(".env should be hidden by default")
	}

	e.toggleHidden()
	if !explorerHasRow(e, ".env") {
		t.Fatalf(".env should appear after toggle; rows=%v", rowRels(e))
	}

	e.toggleHidden()
	if explorerHasRow(e, ".env") {
		t.Fatal(".env should be hidden again after toggling back")
	}
}

func TestExplorer_ReanchorAcrossRefresh(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.txt"), "a")
	writeTestFile(t, filepath.Join(root, "b.txt"), "b")

	e := newTestExplorer(t, root)
	e.cursor = mustRowIndex(t, e, "b.txt")
	e.selectedKey = "b.txt"

	writeTestFile(t, filepath.Join(root, "a2.txt"), "a2")
	e.handleTick()
	if got := e.rows[e.cursor].node.relPath; got != "b.txt" {
		t.Fatalf("selection should stay on b.txt across refresh, got %q", got)
	}

	if err := os.Remove(filepath.Join(root, "b.txt")); err != nil {
		t.Fatal(err)
	}
	e.handleTick()
	if e.cursor < 0 || e.cursor >= len(e.rows) {
		t.Fatalf("cursor out of range after the selected file was removed: %d (rows=%d)", e.cursor, len(e.rows))
	}
}

func TestExplorer_TickPicksUpNewFileInExpandedDir(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "dir", "old.txt"), "o")

	e := newTestExplorer(t, root)
	e.cursor = mustRowIndex(t, e, "dir")
	e.setExpanded(true)

	writeTestFile(t, filepath.Join(root, "dir", "new.txt"), "n")
	e.handleTick()

	if !explorerHasRow(e, "dir/new.txt") {
		t.Fatalf("new file in an expanded dir should appear after a tick; rows=%v", rowRels(e))
	}
}

func TestExplorer_TickIgnoresNewFileInCollapsedDir(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "dir", "old.txt"), "o")

	e := newTestExplorer(t, root) // dir is collapsed by default

	writeTestFile(t, filepath.Join(root, "dir", "new.txt"), "n")
	e.handleTick()

	if explorerHasRow(e, "dir/new.txt") {
		t.Fatal("a file in a collapsed dir should not be read/shown on refresh")
	}
}

func TestExplorer_Navigation(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.txt"), "a")
	writeTestFile(t, filepath.Join(root, "b.txt"), "b")

	e := newTestExplorer(t, root)
	start := e.cursor

	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'}) // nav_down
	if e.cursor != start+1 {
		t.Fatalf("nav_down: cursor = %d, want %d", e.cursor, start+1)
	}
	e.Update(tea.KeyPressMsg{Text: "k", Code: 'k'}) // nav_up
	if e.cursor != start {
		t.Fatalf("nav_up: cursor = %d, want %d", e.cursor, start)
	}
}

func TestExplorer_FuzzyFilterRanking(t *testing.T) {
	root := t.TempDir()
	e := newTestExplorer(t, root)
	e.candidates = []string{"internal/ui/chat.go", "cmd/main.go", "README.md"}

	e.findQuery = "main"
	e.applyFilter()

	if len(e.filtered) == 0 {
		t.Fatal("expected at least one match for 'main'")
	}
	if e.filtered[0] != "cmd/main.go" {
		t.Fatalf("top match = %q, want cmd/main.go", e.filtered[0])
	}
}

func TestExplorer_WalkProjectHonorsGitignore(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".gitignore"), "ignored/\n")
	writeTestFile(t, filepath.Join(root, "ignored", "x.txt"), "x")
	writeTestFile(t, filepath.Join(root, "src", "main.go"), "package main")

	paths, truncated := walkProject(root, false)
	if truncated {
		t.Fatal("small tree should not truncate")
	}
	for _, p := range paths {
		if strings.HasPrefix(p, "ignored") {
			t.Fatalf("walk returned a gitignored path: %q", p)
		}
	}
	want := filepath.Join("src", "main.go")
	if !slices.Contains(paths, want) {
		t.Fatalf("expected %q in walk results: %v", want, paths)
	}
}

func TestExplorer_RevealPathExpandsAncestors(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a", "b", "c.txt"), "c")

	e := newTestExplorer(t, root)
	e.revealPath("a/b/c.txt")

	if !e.expanded["a"] || !e.expanded["a/b"] {
		t.Fatalf("ancestors should be expanded: a=%v a/b=%v", e.expanded["a"], e.expanded["a/b"])
	}
	if got := e.rows[e.cursor].node.relPath; got != "a/b/c.txt" {
		t.Fatalf("cursor should land on the revealed file, got %q", got)
	}
}

func TestExplorer_FindModeKeyCapture(t *testing.T) {
	root := t.TempDir()
	e := newTestExplorer(t, root)
	e.candidates = []string{} // non-nil so enterFind does not kick off a walk

	if cmd := e.enterFind(); cmd != nil {
		t.Fatal("enterFind should not start a walk when candidates are already loaded")
	}
	if !e.findMode {
		t.Fatal("should be in find mode")
	}

	e.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	e.Update(tea.KeyPressMsg{Text: "b", Code: 'b'})
	if e.findQuery != "ab" {
		t.Fatalf("findQuery = %q, want ab", e.findQuery)
	}

	e.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if e.findQuery != "a" {
		t.Fatalf("after backspace findQuery = %q, want a", e.findQuery)
	}

	e.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if e.findMode {
		t.Fatal("esc should exit find mode (back to the tree)")
	}
}

// TestExplorer_RenderSmoke exercises every render path (tree, preview, find) to
// catch panics the model-level tests miss.
func TestExplorer_RenderSmoke(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(root, "dir", "nested.txt"), "hi")

	e := newTestExplorer(t, root)

	if out := e.Render(""); out == "" {
		t.Fatal("tree render should be non-empty")
	}
	if out := e.Render("input-row"); out == "" {
		t.Fatal("render with input row should be non-empty")
	}

	// Select the file and render the preview pane.
	e.cursor = mustRowIndex(t, e, "main.go")
	e.selectedKey = "main.go"
	e.dirtyPreview = true
	if out := e.Render(""); !strings.Contains(out, "main") {
		t.Fatalf("preview should show file content; got %q", out)
	}

	// Find mode render.
	e.candidates = []string{"main.go", "dir/nested.txt"}
	e.enterFind()
	e.findQuery = "main"
	e.applyFilter()
	if out := e.Render(""); out == "" {
		t.Fatal("find-mode render should be non-empty")
	}
}

func TestExplorer_BinaryPreviewPlaceholder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "bin"), "abc\x00def")

	e := newTestExplorer(t, root)
	if out := e.computePreview("bin"); !strings.Contains(out, "Binary or large file") {
		t.Fatalf("expected binary placeholder, got %q", out)
	}
}

func TestExplorer_OversizedPreviewPlaceholder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "big.txt"), strings.Repeat("a", explorerMaxPreviewBytes+1))

	e := newTestExplorer(t, root)
	if out := e.computePreview("big.txt"); !strings.Contains(out, "Binary or large file") {
		t.Fatalf("expected oversized placeholder, got %q", out)
	}
}
