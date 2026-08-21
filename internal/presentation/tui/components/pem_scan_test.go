package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePem(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("key"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestScanPemDir_FindsAndSortsByModTime(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writePem(t, filepath.Join(dir, "old.pem"), now.Add(-48*time.Hour))
	writePem(t, filepath.Join(dir, "new.pem"), now)
	writePem(t, filepath.Join(dir, "sub", "mid.pem"), now.Add(-24*time.Hour))
	writePem(t, filepath.Join(dir, "not-a-key.txt"), now)
	writePem(t, filepath.Join(dir, ".hidden", "secret.pem"), now)
	writePem(t, filepath.Join(dir, "node_modules", "dep.pem"), now)
	writePem(t, filepath.Join(dir, "sub", "deep", "toodeep.pem"), now)

	budget := pemScanMaxEntries
	seen := map[string]bool{}
	var out []pemCandidate
	scanPemDir(dir, 1, &budget, seen, &out)

	if len(out) != 3 {
		t.Fatalf("expected 3 candidates, got %d: %v", len(out), out)
	}
	// scanPemFiles sorts; here verify contents only.
	found := map[string]bool{}
	for _, c := range out {
		found[filepath.Base(c.Path)] = true
	}
	for _, want := range []string{"old.pem", "new.pem", "mid.pem"} {
		if !found[want] {
			t.Errorf("missing %s in %v", want, out)
		}
	}
}

func TestScanPemDir_RespectsBudget(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		writePem(t, filepath.Join(dir, string(rune('a'+i))+".pem"), time.Now())
	}
	budget := 5
	var out []pemCandidate
	scanPemDir(dir, 0, &budget, map[string]bool{}, &out)
	if len(out) > 5 {
		t.Errorf("budget not respected: got %d results", len(out))
	}
}

func TestPemCandidateLabel(t *testing.T) {
	c := pemCandidate{Path: "/tmp/infer-bot.pem", ModTime: time.Now().Add(-49 * time.Hour)}
	label := pemCandidateLabel(c)
	if !strings.Contains(label, "infer-bot.pem") || !strings.Contains(label, "2d ago") {
		t.Errorf("unexpected label: %q", label)
	}
}
