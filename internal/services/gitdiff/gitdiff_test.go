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

	// Two changes far apart so git produces two distinct hunks.
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

	// The staged side should contain only the first change.
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

func TestDiscard(t *testing.T) {
	repo := newTestRepo(t)
	src := NewGitSource(repo)

	// A tracked file modified in the working tree → discard restores it.
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

	// An untracked file → discard deletes it.
	writeFile(t, repo, "junk.txt", "temp\n")
	if err := src.Discard(FileChange{Path: "junk.txt", Status: StatusUntracked}); err != nil {
		t.Fatalf("Discard untracked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "junk.txt")); !os.IsNotExist(err) {
		t.Errorf("junk.txt should be removed after discard, stat err = %v", err)
	}
}
