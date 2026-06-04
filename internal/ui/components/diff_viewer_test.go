package components

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	gitdiff "github.com/inference-gateway/cli/internal/services/gitdiff"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// fakeDiffSource is a hand-written gitdiff.Source for tests.
type fakeDiffSource struct {
	staged, unstaged []gitdiff.FileChange
	diffs            map[string][2]string // path -> {old, new}
	stageCalls       []string
	unstageCalls     []string
	discardCalls     []string
	worktreePatch    gitdiff.FilePatch
	indexPatch       gitdiff.FilePatch
	applyCalls       []applyCall
	workdir          string
}

type applyCall struct {
	hunkIndex int
	reverse   bool
}

func (f *fakeDiffSource) Changes() ([]gitdiff.FileChange, []gitdiff.FileChange, error) {
	return f.staged, f.unstaged, nil
}

func (f *fakeDiffSource) Diff(fc gitdiff.FileChange) (string, string, bool, error) {
	d := f.diffs[fc.Path]
	return d[0], d[1], false, nil
}

func (f *fakeDiffSource) Stage(path string) error {
	f.stageCalls = append(f.stageCalls, path)
	return nil
}

func (f *fakeDiffSource) Unstage(path string) error {
	f.unstageCalls = append(f.unstageCalls, path)
	return nil
}

func (f *fakeDiffSource) WorktreePatch(string) (gitdiff.FilePatch, error) {
	return f.worktreePatch, nil
}

func (f *fakeDiffSource) IndexPatch(string) (gitdiff.FilePatch, error) {
	return f.indexPatch, nil
}

func (f *fakeDiffSource) ApplyHunk(_ gitdiff.FilePatch, hunkIndex int, reverse bool) error {
	f.applyCalls = append(f.applyCalls, applyCall{hunkIndex: hunkIndex, reverse: reverse})
	return nil
}

func (f *fakeDiffSource) Workdir() string { return f.workdir }

func (f *fakeDiffSource) Discard(fc gitdiff.FileChange) error {
	f.discardCalls = append(f.discardCalls, fc.Path)
	return nil
}

func newTestDiffViewer(src *fakeDiffSource) *DiffViewerImpl {
	ts := domain.NewThemeProvider()
	v := NewDiffViewer(src, styles.NewProvider(ts), ts, config.KeybindingsConfig{})
	v.SetWidth(120)
	v.SetHeight(40)
	v.Update(diffViewerLoadedMsg{staged: src.staged, unstaged: src.unstaged})
	return v
}

func fileRowIndex(v *DiffViewerImpl, path string, staged bool) int {
	for i, r := range v.rows {
		if r.kind == rowFile && r.fc.Path == path && r.fc.Staged == staged {
			return i
		}
	}
	return -1
}

func TestDiffViewer_TreeBuild(t *testing.T) {
	src := &fakeDiffSource{
		staged: []gitdiff.FileChange{{Path: "a.go", Status: gitdiff.StatusModified, Staged: true}},
		unstaged: []gitdiff.FileChange{
			{Path: "dir/b.go", Status: gitdiff.StatusModified},
			{Path: "dir/c.go", Status: gitdiff.StatusUntracked},
			{Path: "d.go", Status: gitdiff.StatusModified},
		},
		diffs: map[string][2]string{},
	}
	v := newTestDiffViewer(src)

	if len(v.rows) != 7 {
		t.Fatalf("rows = %d, want 7: %+v", len(v.rows), v.rows)
	}
	if v.rows[0].kind != rowSection || v.rows[0].label != "Staged Changes" || v.rows[0].count != 1 {
		t.Errorf("rows[0] = %+v, want Staged Changes section (count 1)", v.rows[0])
	}
	if v.rows[1].kind != rowFile || v.rows[1].label != "a.go" {
		t.Errorf("rows[1] = %+v, want file a.go", v.rows[1])
	}
	// Folder grouping inside Changes section.
	folderFound := false
	for _, r := range v.rows {
		if r.kind == rowFolder && r.label == "dir" {
			folderFound = true
		}
	}
	if !folderFound {
		t.Errorf("expected a 'dir' folder row")
	}
	// First file should be auto-selected so a diff shows on open.
	if v.cursor != 1 || v.selectedFilePath() != "a.go" {
		t.Errorf("cursor=%d selected=%q, want first file a.go", v.cursor, v.selectedFilePath())
	}
}

func TestDiffViewer_Navigation(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{
			{Path: "a.go", Status: gitdiff.StatusModified},
			{Path: "b.go", Status: gitdiff.StatusModified},
		},
		diffs: map[string][2]string{},
	}
	v := newTestDiffViewer(src)
	start := v.cursor

	v.moveCursor(1)
	if v.cursor != start+1 {
		t.Errorf("after down, cursor=%d want %d", v.cursor, start+1)
	}
	v.moveCursor(-100) // clamps to 0
	if v.cursor != 0 {
		t.Errorf("after clamp up, cursor=%d want 0", v.cursor)
	}
	v.moveCursor(100) // clamps to last
	if v.cursor != len(v.rows)-1 {
		t.Errorf("after clamp down, cursor=%d want %d", v.cursor, len(v.rows)-1)
	}
}

func TestDiffViewer_CollapseSection(t *testing.T) {
	src := &fakeDiffSource{
		staged:   []gitdiff.FileChange{{Path: "a.go", Status: gitdiff.StatusModified, Staged: true}},
		unstaged: []gitdiff.FileChange{{Path: "b.go", Status: gitdiff.StatusModified}},
		diffs:    map[string][2]string{},
	}
	v := newTestDiffViewer(src)
	full := len(v.rows)

	v.cursor = 0 // staged section header
	v.toggleOrSelect()
	if len(v.rows) != full-1 {
		t.Errorf("after collapsing staged section, rows=%d want %d", len(v.rows), full-1)
	}
	v.toggleOrSelect() // re-expand (cursor re-anchored to the same section header)
	if len(v.rows) != full {
		t.Errorf("after re-expanding, rows=%d want %d", len(v.rows), full)
	}
}

func TestDiffViewer_StageUnstage(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{{Path: "a.go", Status: gitdiff.StatusModified}},
		diffs:    map[string][2]string{},
	}
	v := newTestDiffViewer(src)
	v.cursor = fileRowIndex(v, "a.go", false)

	cmd := v.stageCmd()
	if cmd == nil {
		t.Fatal("stageCmd returned nil")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("stage cmd produced nil msg")
	}
	if len(src.stageCalls) != 1 || src.stageCalls[0] != "a.go" {
		t.Errorf("stageCalls = %v, want [a.go]", src.stageCalls)
	}

	cmd = v.unstageCmd()
	if cmd == nil {
		t.Fatal("unstageCmd returned nil")
	}
	cmd()
	if len(src.unstageCalls) != 1 || src.unstageCalls[0] != "a.go" {
		t.Errorf("unstageCalls = %v, want [a.go]", src.unstageCalls)
	}
}

func TestDiffViewer_Commit(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{{Path: "a.go", Status: gitdiff.StatusModified}},
		diffs:    map[string][2]string{},
	}
	v := newTestDiffViewer(src)

	_, cmd := v.commit()
	if !v.IsCancelled() {
		t.Error("commit should mark the panel cancelled to close it")
	}
	if cmd == nil {
		t.Fatal("commit returned nil cmd")
	}
	msg := cmd()
	ev, ok := msg.(domain.UserInputEvent)
	if !ok {
		t.Fatalf("commit cmd msg = %T, want domain.UserInputEvent", msg)
	}
	if ev.Content != "/git commit" {
		t.Errorf("commit content = %q, want /git commit", ev.Content)
	}
}

func TestDiffViewer_RenderShowsSelectedDiff(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{{Path: "main.go", Status: gitdiff.StatusModified}},
		diffs: map[string][2]string{
			"main.go": {"package main\n", "package main\n\nfunc added() {}\n"},
		},
	}
	v := newTestDiffViewer(src)

	out := stripANSI(v.Render(""))
	if !strings.Contains(out, "Changes") {
		t.Errorf("sidebar section header missing:\n%s", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected file name in output:\n%s", out)
	}
	if !strings.Contains(out, "added") {
		t.Errorf("expected added diff content in output:\n%s", out)
	}
}

func TestDiffViewer_PatchModeStage(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{{Path: "main.go", Status: gitdiff.StatusModified}},
		diffs:    map[string][2]string{},
		worktreePatch: gitdiff.FilePatch{
			Preamble: "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go",
			Hunks: []gitdiff.Hunk{
				{Header: "@@ -1,2 +1,3 @@", Lines: []string{" ctx", "+addedHunkZero"}},
				{Header: "@@ -10,1 +11,1 @@", Lines: []string{"-removedOne", "+addedHunkOne"}},
			},
		},
	}
	v := newTestDiffViewer(src)

	enter := v.enterPatchCmd()
	if enter == nil {
		t.Fatal("enterPatchCmd returned nil")
	}
	v.Update(enter())

	if !v.patchMode {
		t.Fatal("expected patchMode after entering")
	}
	if len(v.patchFile.Hunks) != 2 {
		t.Fatalf("patch hunks = %d, want 2", len(v.patchFile.Hunks))
	}
	if !strings.Contains(v.HintText(), "hunk") {
		t.Errorf("patch-mode hint = %q, want it to mention hunks", v.HintText())
	}

	out := stripANSI(v.Render(""))
	if !strings.Contains(out, "addedHunkZero") {
		t.Errorf("patch render missing hunk 0 content:\n%s", out)
	}

	v.movePatchHunk(1)
	if v.patchHunk != 1 {
		t.Errorf("patchHunk = %d, want 1", v.patchHunk)
	}

	apply := v.applyHunkCmd()
	if apply == nil {
		t.Fatal("applyHunkCmd returned nil")
	}
	apply()
	if len(src.applyCalls) != 1 {
		t.Fatalf("applyCalls = %d, want 1", len(src.applyCalls))
	}
	if src.applyCalls[0].hunkIndex != 1 || src.applyCalls[0].reverse {
		t.Errorf("applyCalls[0] = %+v, want {hunkIndex:1 reverse:false}", src.applyCalls[0])
	}
}

func TestDiffViewer_PatchModeUnstageDirection(t *testing.T) {
	src := &fakeDiffSource{
		staged: []gitdiff.FileChange{{Path: "main.go", Status: gitdiff.StatusModified, Staged: true}},
		diffs:  map[string][2]string{},
		indexPatch: gitdiff.FilePatch{
			Preamble: "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go",
			Hunks:    []gitdiff.Hunk{{Header: "@@ -1,1 +1,1 @@", Lines: []string{"-a", "+b"}}},
		},
	}
	v := newTestDiffViewer(src)

	v.Update(v.enterPatchCmd()())
	if !v.patchMode || !v.patchStaged {
		t.Fatalf("expected patch mode on a staged file (patchMode=%v staged=%v)", v.patchMode, v.patchStaged)
	}

	v.applyHunkCmd()()
	if len(src.applyCalls) != 1 || !src.applyCalls[0].reverse {
		t.Errorf("applyCalls = %+v, want a single reverse (unstage) apply", src.applyCalls)
	}
}

func TestDiffViewer_EmptyState(t *testing.T) {
	src := &fakeDiffSource{diffs: map[string][2]string{}}
	v := newTestDiffViewer(src)
	if v.hasAnyFile() {
		t.Fatal("hasAnyFile = true on empty source")
	}
	out := stripANSI(v.Render(""))
	if !strings.Contains(out, "No changes") {
		t.Errorf("expected 'No changes' empty state:\n%s", out)
	}
}

func TestDiffViewer_ConfigurableKeybinding(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{{Path: "a.go", Status: gitdiff.StatusModified}},
		diffs:    map[string][2]string{},
	}
	ts := domain.NewThemeProvider()
	kb := config.KeybindingsConfig{Bindings: map[string]config.KeyBindingEntry{
		config.ActionID(config.NamespaceDiffViewer, "stage"): {Keys: []string{"g"}},
	}}
	v := NewDiffViewer(src, styles.NewProvider(ts), ts, kb)
	v.SetWidth(120)
	v.SetHeight(40)
	v.Update(diffViewerLoadedMsg{unstaged: src.unstaged})
	v.cursor = fileRowIndex(v, "a.go", false)

	// The old default 'a' no longer stages after the rebinding.
	if _, cmd := v.Update(tea.KeyPressMsg{Text: "a", Code: 'a'}); cmd != nil {
		t.Error("default 'a' should no longer stage after rebinding stage->g")
	}

	// The rebound 'g' stages the selected file.
	_, cmd := v.Update(tea.KeyPressMsg{Text: "g", Code: 'g'})
	if cmd == nil {
		t.Fatal("rebound 'g' should produce a stage cmd")
	}
	cmd()
	if len(src.stageCalls) != 1 || src.stageCalls[0] != "a.go" {
		t.Errorf("stageCalls = %v, want [a.go]", src.stageCalls)
	}

	// The footer hint reflects the user's binding.
	if !strings.Contains(v.HintText(), "g stage") {
		t.Errorf("hint = %q, want it to show 'g stage'", v.HintText())
	}
}

func TestDiffViewer_EditKeyBound(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{{Path: "a.go", Status: gitdiff.StatusModified}},
		diffs:    map[string][2]string{},
	}
	v := newTestDiffViewer(src)
	// The edit action is wired to the configurable key (default `v`); assert the
	// binding without launching a real editor (that needs a PTY + a TTY editor).
	if v.keymap.match("v", actDiffEdit) != actDiffEdit {
		t.Errorf("default 'v' should be bound to the diff-viewer edit action")
	}
}

func TestDiffViewer_EditSkipsDeleted(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{{Path: "gone.go", Status: gitdiff.StatusDeleted}},
		diffs:    map[string][2]string{},
		workdir:  t.TempDir(),
	}
	v := newTestDiffViewer(src)
	v.cursor = fileRowIndex(v, "gone.go", false)

	if cmd := v.enterEditCmd(); cmd != nil {
		t.Error("enterEditCmd should be a no-op for a deleted file")
	}
	if v.editMode {
		t.Error("a deleted file must not open the editor")
	}
}

func TestDiffViewer_DiscardConfirm(t *testing.T) {
	src := &fakeDiffSource{
		unstaged: []gitdiff.FileChange{{Path: "a.go", Status: gitdiff.StatusModified}},
		diffs:    map[string][2]string{},
	}
	v := newTestDiffViewer(src)
	v.cursor = fileRowIndex(v, "a.go", false)

	// `d` arms a confirmation; it must not discard yet.
	v.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	if v.confirmDiscard == nil {
		t.Fatal("pressing d should arm a discard confirmation")
	}
	if !strings.Contains(v.HintText(), "discard") {
		t.Errorf("hint = %q, want a discard confirmation", v.HintText())
	}
	if out := stripANSI(v.Render("")); !strings.Contains(out, "Discard changes to a.go") {
		t.Errorf("expected a discard prompt in the pane:\n%s", out)
	}

	// `n` cancels without discarding.
	v.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	if v.confirmDiscard != nil || len(src.discardCalls) != 0 {
		t.Errorf("n should cancel without discarding; calls=%v", src.discardCalls)
	}

	// `d` then `y` discards the file.
	v.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	_, cmd := v.Update(tea.KeyPressMsg{Text: "y", Code: 'y'})
	if cmd == nil {
		t.Fatal("y should produce a discard cmd")
	}
	cmd()
	if len(src.discardCalls) != 1 || src.discardCalls[0] != "a.go" {
		t.Errorf("discardCalls = %v, want [a.go]", src.discardCalls)
	}
}
