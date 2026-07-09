package gitdiff

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRepo creates a git repo in a temp dir with one committed file
// ("tracked.txt" containing "line1\nline2\n") and returns its path.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeFile(t, dir, "tracked.txt", "line1\nline2\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed; skipping git-backed test")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func findChange(list []FileChange, path string) (FileChange, bool) {
	for _, fc := range list {
		if fc.Path == path {
			return fc, true
		}
	}
	return FileChange{}, false
}

func TestRangeSource_PRDiff(t *testing.T) {
	repo := newTestRepo(t)
	base := revParse(t, repo, "HEAD")
	writeFile(t, repo, "tracked.txt", "line1\nline2\nfeat\n")
	writeFile(t, repo, "feature.txt", "brand new\n")
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-q", "-m", "feature")
	writeFile(t, repo, "tracked.txt", "line1\nline2\nfeat\nuncommitted\n")

	src := newPRSourceWithBase(repo, base)
	if src.Workdir() != repo {
		t.Errorf("Workdir = %q, want %q", src.Workdir(), repo)
	}

	staged, unstaged, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(staged) != 0 {
		t.Errorf("staged = %v, want empty (PR tab has no staged group)", staged)
	}

	modified, ok := findChange(unstaged, "tracked.txt")
	if !ok || modified.Status != StatusModified {
		t.Fatalf("tracked.txt = %+v (ok=%v), want modified", modified, ok)
	}
	oldC, newC, isBin, err := src.Diff(modified)
	if err != nil || isBin {
		t.Fatalf("Diff err=%v isBin=%v", err, isBin)
	}
	if oldC != "line1\nline2\n" {
		t.Errorf("old = %q, want base content", oldC)
	}
	if newC != "line1\nline2\nfeat\nuncommitted\n" {
		t.Errorf("new = %q, want working-tree content (committed + uncommitted)", newC)
	}

	added, ok := findChange(unstaged, "feature.txt")
	if !ok || added.Status != StatusAdded {
		t.Fatalf("feature.txt = %+v (ok=%v), want added", added, ok)
	}
	oldC, _, _, err = src.Diff(added)
	if err != nil {
		t.Fatalf("Diff added: %v", err)
	}
	if oldC != "" {
		t.Errorf("added old = %q, want empty", oldC)
	}
}

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func TestIsRepo(t *testing.T) {
	repo := newTestRepo(t)
	if !IsRepo(repo) {
		t.Errorf("IsRepo(repo) = false, want true")
	}
	if IsRepo(t.TempDir()) {
		t.Errorf("IsRepo(non-repo) = true, want false")
	}
}

func TestChanges_ModifiedUnstaged(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, repo, "tracked.txt", "line1\nline2\nline3\n")

	src := NewGitSource(repo)
	staged, unstaged, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(staged) != 0 {
		t.Errorf("staged = %v, want empty", staged)
	}
	fc, ok := findChange(unstaged, "tracked.txt")
	if !ok || fc.Status != StatusModified || fc.Staged {
		t.Fatalf("unstaged tracked.txt = %+v (ok=%v), want modified/unstaged", fc, ok)
	}

	oldC, newC, isBin, err := src.Diff(fc)
	if err != nil || isBin {
		t.Fatalf("Diff err=%v isBin=%v", err, isBin)
	}
	if oldC != "line1\nline2\n" || newC != "line1\nline2\nline3\n" {
		t.Errorf("Diff old=%q new=%q", oldC, newC)
	}
}

func TestChanges_StagedNewFile(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, repo, "added.txt", "hello\n")
	runGit(t, repo, "add", "added.txt")

	src := NewGitSource(repo)
	staged, _, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	fc, ok := findChange(staged, "added.txt")
	if !ok || fc.Status != StatusAdded || !fc.Staged {
		t.Fatalf("staged added.txt = %+v (ok=%v), want added/staged", fc, ok)
	}

	oldC, newC, _, err := src.Diff(fc)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if oldC != "" || newC != "hello\n" {
		t.Errorf("Diff old=%q new=%q, want ''/'hello\\n'", oldC, newC)
	}
}

func TestChanges_Untracked(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, repo, "sub/new.txt", "fresh\n")

	src := NewGitSource(repo)
	_, unstaged, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	fc, ok := findChange(unstaged, "sub/new.txt")
	if !ok || fc.Status != StatusUntracked {
		t.Fatalf("untracked sub/new.txt = %+v (ok=%v)", fc, ok)
	}

	oldC, newC, _, err := src.Diff(fc)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if oldC != "" || newC != "fresh\n" {
		t.Errorf("Diff old=%q new=%q", oldC, newC)
	}
}

func TestChanges_StagedAndUnstaged(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, repo, "tracked.txt", "line1\nstaged\n")
	runGit(t, repo, "add", "tracked.txt")
	writeFile(t, repo, "tracked.txt", "line1\nstaged\nunstaged\n")

	src := NewGitSource(repo)
	staged, unstaged, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if _, ok := findChange(staged, "tracked.txt"); !ok {
		t.Errorf("expected tracked.txt in staged group")
	}
	if _, ok := findChange(unstaged, "tracked.txt"); !ok {
		t.Errorf("expected tracked.txt in unstaged group")
	}
}

func TestChanges_DeletedUnstaged(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.Remove(filepath.Join(repo, "tracked.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	src := NewGitSource(repo)
	_, unstaged, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	fc, ok := findChange(unstaged, "tracked.txt")
	if !ok || fc.Status != StatusDeleted {
		t.Fatalf("deleted tracked.txt = %+v (ok=%v)", fc, ok)
	}

	oldC, newC, _, err := src.Diff(fc)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if oldC != "line1\nline2\n" || newC != "" {
		t.Errorf("Diff old=%q new=%q, want full/''", oldC, newC)
	}
}

func TestParsePatch(t *testing.T) {
	patch := "diff --git a/f.txt b/f.txt\n" +
		"index 111..222 100644\n" +
		"--- a/f.txt\n" +
		"+++ b/f.txt\n" +
		"@@ -1,2 +1,2 @@\n" +
		" ctx\n" +
		"-old\n" +
		"+new\n" +
		"@@ -10,2 +10,3 @@\n" +
		" tail\n" +
		"+added\n"

	fp := parsePatch(patch)
	if len(fp.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(fp.Hunks))
	}
	if !strings.HasPrefix(fp.Preamble, "diff --git") || !strings.Contains(fp.Preamble, "+++ b/f.txt") {
		t.Errorf("preamble missing headers: %q", fp.Preamble)
	}
	if fp.Hunks[0].Header != "@@ -1,2 +1,2 @@" {
		t.Errorf("hunk0 header = %q", fp.Hunks[0].Header)
	}
	if fp.Hunks[1].Header != "@@ -10,2 +10,3 @@" {
		t.Errorf("hunk1 header = %q", fp.Hunks[1].Header)
	}
}

// writeNumberedFile writes n lines "lineN\n".
func writeNumberedFile(t *testing.T, dir, name string, n int) {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	writeFile(t, dir, name, b.String())
}

func TestApplyHunk_StagesSingleHunk(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeNumberedFile(t, dir, "f.txt", 20)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")

	content := readFileLines(t, dir, "f.txt")
	content[0] = "CHANGED1"
	content[19] = "CHANGED20"
	writeFileLines(t, dir, "f.txt", content)

	src := NewGitSource(dir)
	fp, err := src.WorktreePatch("f.txt")
	if err != nil {
		t.Fatalf("WorktreePatch: %v", err)
	}
	if len(fp.Hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(fp.Hunks))
	}

	if err := src.ApplyHunk(fp, 0, false); err != nil {
		t.Fatalf("ApplyHunk(0): %v", err)
	}

	staged, unstaged, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if _, ok := findChange(staged, "f.txt"); !ok {
		t.Errorf("f.txt should be staged after applying hunk 0")
	}
	if _, ok := findChange(unstaged, "f.txt"); !ok {
		t.Errorf("f.txt should still have unstaged changes (hunk 1)")
	}

	sfc, _ := findChange(staged, "f.txt")
	_, newC, _, err := src.Diff(sfc)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(newC, "CHANGED1") {
		t.Errorf("staged content missing CHANGED1:\n%s", newC)
	}
	if strings.Contains(newC, "CHANGED20") {
		t.Errorf("staged content unexpectedly contains CHANGED20 (hunk 1 not staged):\n%s", newC)
	}
}

func TestApplyHunk_Reverse_Unstages(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeNumberedFile(t, dir, "f.txt", 20)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")

	content := readFileLines(t, dir, "f.txt")
	content[0] = "CHANGED1"
	writeFileLines(t, dir, "f.txt", content)
	runGit(t, dir, "add", "f.txt")

	src := NewGitSource(dir)
	fp, err := src.IndexPatch("f.txt")
	if err != nil {
		t.Fatalf("IndexPatch: %v", err)
	}
	if len(fp.Hunks) == 0 {
		t.Fatal("expected staged hunks")
	}

	if err := src.ApplyHunk(fp, 0, true); err != nil {
		t.Fatalf("ApplyHunk reverse: %v", err)
	}

	staged, _, _ := src.Changes()
	if _, ok := findChange(staged, "f.txt"); ok {
		t.Errorf("f.txt should no longer be staged after reversing its only hunk")
	}
}

func TestApplyLines_StagesOnlySelectedChange(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeNumberedFile(t, dir, "f.txt", 8)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")

	content := readFileLines(t, dir, "f.txt")
	content[2] = "CHANGED3"
	content[4] = "CHANGED5"
	writeFileLines(t, dir, "f.txt", content)

	src := NewGitSource(dir)
	fp, err := src.WorktreePatch("f.txt")
	if err != nil {
		t.Fatalf("WorktreePatch: %v", err)
	}
	if len(fp.Hunks) != 1 {
		t.Fatalf("expected the two edits to share one hunk, got %d", len(fp.Hunks))
	}

	sel := selectLines(fp.Hunks[0], "-line3", "+CHANGED3")
	if err := src.ApplyLines(fp, 0, sel, false); err != nil {
		t.Fatalf("ApplyLines: %v", err)
	}

	staged, unstaged, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	sfc, ok := findChange(staged, "f.txt")
	if !ok {
		t.Fatal("f.txt should be staged")
	}
	_, newC, _, err := src.Diff(sfc)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(newC, "CHANGED3") {
		t.Errorf("staged content should include the selected edit CHANGED3:\n%s", newC)
	}
	if strings.Contains(newC, "CHANGED5") {
		t.Errorf("staged content should NOT include the unselected edit CHANGED5:\n%s", newC)
	}
	if _, ok := findChange(unstaged, "f.txt"); !ok {
		t.Error("f.txt should still have the unselected edit unstaged")
	}
}

func TestApplyLines_Reverse_UnstagesOnlySelectedChange(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeNumberedFile(t, dir, "f.txt", 8)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")

	content := readFileLines(t, dir, "f.txt")
	content[2] = "CHANGED3"
	content[4] = "CHANGED5"
	writeFileLines(t, dir, "f.txt", content)
	runGit(t, dir, "add", "f.txt")

	src := NewGitSource(dir)
	fp, err := src.IndexPatch("f.txt")
	if err != nil {
		t.Fatalf("IndexPatch: %v", err)
	}
	if len(fp.Hunks) != 1 {
		t.Fatalf("expected one staged hunk, got %d", len(fp.Hunks))
	}

	sel := selectLines(fp.Hunks[0], "-line3", "+CHANGED3")
	if err := src.ApplyLines(fp, 0, sel, true); err != nil {
		t.Fatalf("ApplyLines reverse: %v", err)
	}

	staged, unstaged, err := src.Changes()
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	sfc, ok := findChange(staged, "f.txt")
	if !ok {
		t.Fatal("f.txt should still be staged (CHANGED5 remains)")
	}
	_, newC, _, err := src.Diff(sfc)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(newC, "CHANGED5") {
		t.Errorf("staged content should still include CHANGED5:\n%s", newC)
	}
	if strings.Contains(newC, "CHANGED3") {
		t.Errorf("staged content should no longer include the unstaged edit CHANGED3:\n%s", newC)
	}
	if _, ok := findChange(unstaged, "f.txt"); !ok {
		t.Error("the unstaged edit (CHANGED3) should now appear as a working-tree change")
	}
}

// selectLines returns the indices of the given verbatim patch lines within a
// hunk, for selecting specific changes in ApplyLines tests.
func selectLines(h Hunk, want ...string) map[int]bool {
	sel := map[int]bool{}
	for i, l := range h.Lines {
		for _, w := range want {
			if l == w {
				sel[i] = true
			}
		}
	}
	return sel
}

func TestSplitHunk(t *testing.T) {
	h := Hunk{
		Header: "@@ -1,7 +1,7 @@ func foo()",
		Lines: []string{
			" line1", " line2",
			"-line3", "+CHANGED3",
			" line4",
			"-line5", "+CHANGED5",
			" line6", " line7",
		},
	}
	pieces := splitHunk(h)
	if len(pieces) != 2 {
		t.Fatalf("expected 2 pieces, got %d", len(pieces))
	}
	if pieces[0].Header != "@@ -1,4 +1,4 @@ func foo()" {
		t.Errorf("piece 0 header = %q, want @@ -1,4 +1,4 @@ func foo()", pieces[0].Header)
	}
	if pieces[1].Header != "@@ -4,4 +4,4 @@ func foo()" {
		t.Errorf("piece 1 header = %q, want @@ -4,4 +4,4 @@ func foo()", pieces[1].Header)
	}
	if last := pieces[0].Lines[len(pieces[0].Lines)-1]; last != " line4" {
		t.Errorf("piece 0 should end with shared context %q, got %q", " line4", last)
	}
	if pieces[1].Lines[0] != " line4" {
		t.Errorf("piece 1 should start with shared context %q, got %q", " line4", pieces[1].Lines[0])
	}

	single := Hunk{Header: "@@ -1,3 +1,3 @@", Lines: []string{" a", "-b", "+B", " c"}}
	if got := splitHunk(single); len(got) != 1 {
		t.Errorf("single-run hunk should not split, got %d pieces", len(got))
	}
}

func readFileLines(t *testing.T, dir, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func writeFileLines(t *testing.T, dir, name string, lines []string) {
	t.Helper()
	writeFile(t, dir, name, strings.Join(lines, "\n")+"\n")
}

func TestStageUnstage(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, repo, "tracked.txt", "line1\nline2\nchanged\n")
	src := NewGitSource(repo)

	if err := src.Stage("tracked.txt"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	staged, _, _ := src.Changes()
	if _, ok := findChange(staged, "tracked.txt"); !ok {
		t.Fatalf("after Stage, tracked.txt not in staged group")
	}

	if err := src.Unstage("tracked.txt"); err != nil {
		t.Fatalf("Unstage: %v", err)
	}
	staged, unstaged, _ := src.Changes()
	if _, ok := findChange(staged, "tracked.txt"); ok {
		t.Errorf("after Unstage, tracked.txt still staged")
	}
	if _, ok := findChange(unstaged, "tracked.txt"); !ok {
		t.Errorf("after Unstage, tracked.txt not in unstaged group")
	}
}

func TestStageUnstageAll(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, repo, "tracked.txt", "line1\nline2\nchanged\n")
	writeFile(t, repo, "new.txt", "hello\n")
	src := NewGitSource(repo)

	if err := src.StageAll(); err != nil {
		t.Fatalf("StageAll: %v", err)
	}
	staged, unstaged, _ := src.Changes()
	for _, p := range []string{"tracked.txt", "new.txt"} {
		if _, ok := findChange(staged, p); !ok {
			t.Errorf("after StageAll, %s not in staged group", p)
		}
	}
	if len(unstaged) != 0 {
		t.Errorf("after StageAll, unstaged group should be empty, got %v", unstaged)
	}

	if err := src.UnstageAll(); err != nil {
		t.Fatalf("UnstageAll: %v", err)
	}
	staged, unstaged, _ = src.Changes()
	if len(staged) != 0 {
		t.Errorf("after UnstageAll, staged group should be empty, got %v", staged)
	}
	for _, p := range []string{"tracked.txt", "new.txt"} {
		if _, ok := findChange(unstaged, p); !ok {
			t.Errorf("after UnstageAll, %s not in unstaged group", p)
		}
	}
}

func TestDiscard(t *testing.T) {
	repo := newTestRepo(t)
	src := NewGitSource(repo)

	writeFile(t, repo, "tracked.txt", "line1\nline2\nMODIFIED\n")
	if err := src.Discard(FileChange{Path: "tracked.txt", Status: StatusModified}); err != nil {
		t.Fatalf("Discard tracked: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, "tracked.txt")); string(got) != "line1\nline2\n" {
		t.Errorf("after discard, tracked.txt = %q, want original", got)
	}
	_, unstaged, _ := src.Changes()
	if _, ok := findChange(unstaged, "tracked.txt"); ok {
		t.Errorf("tracked.txt should have no unstaged changes after discard")
	}

	writeFile(t, repo, "junk.txt", "temp\n")
	if err := src.Discard(FileChange{Path: "junk.txt", Status: StatusUntracked}); err != nil {
		t.Fatalf("Discard untracked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "junk.txt")); !os.IsNotExist(err) {
		t.Errorf("junk.txt should be removed after discard, stat err = %v", err)
	}
}
