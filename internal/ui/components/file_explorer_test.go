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

// selectFileForPreview positions the explorer cursor on rel and renders once so
// the preview pane (and previewLines) is populated. Returns the explorer for
// chaining.
func selectFileForPreview(t *testing.T, e *FileExplorerImpl, rel string) *FileExplorerImpl {
	t.Helper()
	e.cursor = mustRowIndex(t, e, rel)
	e.selectedKey = rel
	e.dirtyPreview = true
	e.Render("") // triggers ensurePreview → sets previewLines
	return e
}

func TestExplorer_EnterSelectMode(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "main.go")

	if e.previewLines <= 0 {
		t.Fatalf("previewLines = %d, want > 0", e.previewLines)
	}

	e.enterSelectMode()
	if !e.selectMode {
		t.Fatal("enterSelectMode should set selectMode=true")
	}
	if e.selAnchor != -1 {
		t.Fatalf("selAnchor = %d, want -1", e.selAnchor)
	}
	if e.previewCursor < 0 || e.previewCursor >= e.previewLines {
		t.Fatalf("previewCursor = %d, want in [0,%d]", e.previewCursor, e.previewLines-1)
	}
}

func TestExplorer_EnterSelectModeNoFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n")
	e := newTestExplorer(t, root)

	// No file selected (cursor on a dir or nothing).
	e.enterSelectMode()
	if e.selectMode {
		t.Fatal("enterSelectMode should be a no-op when no file is previewed")
	}
}

func TestExplorer_PreviewCursorMovement(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "f.go"), "a\nb\nc\nd\ne\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "f.go")
	e.enterSelectMode()

	// Move down a few lines.
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	if e.previewCursor != 2 {
		t.Fatalf("after 2x nav_down previewCursor = %d, want 2", e.previewCursor)
	}

	// Move back up.
	e.Update(tea.KeyPressMsg{Text: "k", Code: 'k'})
	if e.previewCursor != 1 {
		t.Fatalf("after nav_up previewCursor = %d, want 1", e.previewCursor)
	}

	// Clamp at bottom.
	e.previewCursor = e.previewLines - 1
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	if e.previewCursor != e.previewLines-1 {
		t.Fatalf("nav_down at bottom: previewCursor = %d, want %d", e.previewCursor, e.previewLines-1)
	}

	// Clamp at top.
	e.previewCursor = 0
	e.Update(tea.KeyPressMsg{Text: "k", Code: 'k'})
	if e.previewCursor != 0 {
		t.Fatalf("nav_up at top: previewCursor = %d, want 0", e.previewCursor)
	}
}

func TestExplorer_ToggleRangeSelection(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "f.go"), "a\nb\nc\nd\ne\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "f.go")
	e.enterSelectMode()

	// Anchor at line 0.
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '}) // toggle_select (space)
	if e.selAnchor != 0 {
		t.Fatalf("after toggle_select selAnchor = %d, want 0", e.selAnchor)
	}

	// Move cursor to line 2.
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})

	lo, hi, ok := e.previewSelectionRange()
	if !ok || lo != 0 || hi != 2 {
		t.Fatalf("previewSelectionRange = (%d,%d,%v), want (0,2,true)", lo, hi, ok)
	}

	// Toggle again clears the anchor.
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '})
	if e.selAnchor != -1 {
		t.Fatalf("after second toggle selAnchor = %d, want -1", e.selAnchor)
	}
	_, _, ok = e.previewSelectionRange()
	if ok {
		t.Fatal("previewSelectionRange should report no selection after clear")
	}
}

func TestExplorer_AnnotateConfirmStoresSelection(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "f.go"), "a\nb\nc\nd\ne\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "f.go")
	e.enterSelectMode()

	// Anchor at line 0, move to line 1.
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '})
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})

	// Enter annotate mode.
	e.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	if !e.annotateMode {
		t.Fatal("annotate key should enter annotate mode")
	}

	// Type the instruction.
	e.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})
	e.Update(tea.KeyPressMsg{Text: "e", Code: 'e'})
	e.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	if e.annotateInput != "ref" {
		t.Fatalf("annotateInput = %q, want ref", e.annotateInput)
	}

	// Confirm with enter.
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if e.annotateMode {
		t.Fatal("enter should exit annotate mode")
	}
	sels := e.Selections()
	if len(sels) != 1 {
		t.Fatalf("Selections = %d, want 1", len(sels))
	}
	s := sels[0]
	if s.File != "f.go" {
		t.Errorf("File = %q, want f.go", s.File)
	}
	if s.StartLine != 1 || s.EndLine != 2 {
		t.Errorf("StartLine/EndLine = %d/%d, want 1/2 (1-indexed inclusive)", s.StartLine, s.EndLine)
	}
	if s.Annotation != "ref" {
		t.Errorf("Annotation = %q, want ref", s.Annotation)
	}
}

func TestExplorer_AnnotateEscapeDiscards(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "f.go"), "a\nb\nc\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "f.go")
	e.enterSelectMode()

	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '}) // anchor
	e.Update(tea.KeyPressMsg{Text: "a", Code: 'a'}) // annotate
	e.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	e.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if e.annotateMode {
		t.Fatal("esc should exit annotate mode")
	}
	if len(e.Selections()) != 0 {
		t.Fatalf("Selections = %d, want 0 after esc discard", len(e.Selections()))
	}
	// Anchor is retained so the user can retry.
	if e.selAnchor < 0 {
		t.Fatal("selAnchor should be retained after annotate esc")
	}
}

func TestExplorer_MultipleSelectionsAcrossFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.go"), "a\nb\nc\n")
	writeTestFile(t, filepath.Join(root, "b.go"), "d\ne\nf\n")
	e := newTestExplorer(t, root)

	// File A: select+annotate lines 1-2.
	selectFileForPreview(t, e, "a.go")
	e.enterSelectMode()
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '})
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	e.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	e.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Exit select mode, then navigate to file B (tree nav).
	e.Update(tea.KeyPressMsg{Code: tea.KeyEscape})  // exit select mode
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'}) // nav_down in tree

	// File B: select+annotate line 1.
	selectFileForPreview(t, e, "b.go")
	e.enterSelectMode()
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '})
	e.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	e.Update(tea.KeyPressMsg{Text: "y", Code: 'y'})
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	sels := e.Selections()
	if len(sels) != 2 {
		t.Fatalf("Selections = %d, want 2", len(sels))
	}
	if sels[0].File != "a.go" || sels[1].File != "b.go" {
		t.Fatalf("Files = %q,%q want a.go,b.go", sels[0].File, sels[1].File)
	}
	if sels[0].Annotation != "x" || sels[1].Annotation != "y" {
		t.Fatalf("Annotations = %q,%q want x,y", sels[0].Annotation, sels[1].Annotation)
	}
}

func TestExplorer_SubmitSetsDone(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "f.go"), "a\nb\nc\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "f.go")
	e.enterSelectMode()

	// Submit with no selections → no-op.
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if e.IsDone() {
		t.Fatal("submit with no selections should not set done")
	}

	// Annotate one range.
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '})
	e.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	e.Update(tea.KeyPressMsg{Text: "z", Code: 'z'})
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Now submit.
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !e.IsDone() {
		t.Fatal("submit with selections should set done=true")
	}
}

func TestExplorer_EscExitsSelectMode(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "f.go"), "a\nb\nc\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "f.go")
	e.enterSelectMode()
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '}) // anchor a range
	e.selections = append(e.selections, SnippetSelection{File: "f.go", StartLine: 1, EndLine: 1, Annotation: "prior"})

	e.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if e.selectMode {
		t.Fatal("esc should exit select mode")
	}
	if e.IsCancelled() {
		t.Fatal("esc in select mode should not cancel the explorer")
	}
	if len(e.selections) != 1 {
		t.Fatalf("selections should be preserved after esc, got %d", len(e.selections))
	}
}

func TestExplorer_PreviewHighlightRender(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "f.go"), "line1\nline2\nline3\nline4\nline5\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "f.go")
	e.enterSelectMode()

	// Select lines 2-3 (0-indexed 1-2).
	e.previewCursor = 1
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '}) // anchor at 1
	e.Update(tea.KeyPressMsg{Text: "j", Code: 'j'}) // cursor → 2

	out := e.Render("")
	if !strings.Contains(out, "▌") {
		t.Fatal("select-mode render should contain the ▌ selection gutter marker")
	}
	if !strings.Contains(out, "▶") {
		t.Fatal("select-mode render should contain the ▶ cursor gutter marker")
	}
}

func TestExplorer_AnnotateModeRenderSmoke(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "f.go"), "line1\nline2\nline3\n")
	e := newTestExplorer(t, root)
	selectFileForPreview(t, e, "f.go")
	e.enterSelectMode()
	e.Update(tea.KeyPressMsg{Text: " ", Code: ' '})
	e.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	e.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})

	if !e.annotateMode {
		t.Fatal("should be in annotate mode")
	}
	out := e.Render("")
	if !strings.Contains(out, "Annotate") {
		t.Fatalf("annotate-mode render should contain 'Annotate' prompt, got %q", out)
	}
	if !strings.Contains(out, "r") {
		t.Fatal("annotate-mode render should show typed text")
	}
}

func TestExplorer_FormatAnnotations(t *testing.T) {
	root := t.TempDir()
	content := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	writeTestFile(t, filepath.Join(root, "main.go"), content)

	sels := []SnippetSelection{
		{File: "main.go", StartLine: 3, EndLine: 5, Annotation: "refactor to use early returns"},
	}
	out := FormatAnnotations(root, sels)

	checks := []string{
		"# Code annotations",
		"main.go",
		"```go",
		"func main()",
		"Lines 3-5: refactor to use early returns",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("FormatAnnotations output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestExplorer_FormatAnnotationsLargeFileWindows(t *testing.T) {
	root := t.TempDir()
	// File larger than explorerMaxPreviewBytes so the windowed path is taken.
	content := strings.Repeat("line\n", explorerMaxPreviewBytes/5+10)
	writeTestFile(t, filepath.Join(root, "big.txt"), content)

	sels := []SnippetSelection{
		{File: "big.txt", StartLine: 10, EndLine: 12, Annotation: "fix this"},
	}
	out := FormatAnnotations(root, sels)

	if !strings.Contains(out, "Lines 10-12") {
		t.Errorf("windowed output should reference lines 10-12\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "fix this") {
		t.Errorf("windowed output should contain annotation\n--- output ---\n%s", out)
	}
	// Should NOT include the full file (no line 1 with the full file block).
	// The windowed path emits "### Lines" headers; the full-file path does not.
	if !strings.Contains(out, "### Lines") {
		t.Errorf("windowed output should use ### Lines header\n--- output ---\n%s", out)
	}
}

func TestExplorer_FormatAnnotationsMultiFileAndMissing(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.go"), "a\nb\nc\n")
	writeTestFile(t, filepath.Join(root, "b.go"), "d\ne\nf\n")

	sels := []SnippetSelection{
		{File: "a.go", StartLine: 1, EndLine: 2, Annotation: "first"},
		{File: "b.go", StartLine: 2, EndLine: 3, Annotation: "second"},
		{File: "gone.go", StartLine: 1, EndLine: 1, Annotation: "missing"},
	}
	out := FormatAnnotations(root, sels)

	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Errorf("output should list both files\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Errorf("output should contain both annotations\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "unavailable") {
		t.Errorf("output should mention the missing file\n--- output ---\n%s", out)
	}
}

func TestExplorer_FormatAnnotationsEmpty(t *testing.T) {
	if out := FormatAnnotations(t.TempDir(), nil); out != "" {
		t.Fatalf("FormatAnnotations(nil) = %q, want empty", out)
	}
}
