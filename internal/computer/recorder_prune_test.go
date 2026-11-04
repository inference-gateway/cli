package computer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneRecordingsKeepsTheNewestUnderTheCap(t *testing.T) {
	dir := t.TempDir()
	for i, name := range []string{"old.mp4", "mid.mp4", "new.mp4", "notes.txt"} {
		path := filepath.Join(dir, name)
		_ = os.WriteFile(path, make([]byte, 100), 0o600)
		stamp := time.Now().Add(time.Duration(i-3) * time.Hour)
		_ = os.Chtimes(path, stamp, stamp)
	}

	pruneRecordings(dir, 250)

	left, _ := os.ReadDir(dir)
	var names []string
	for _, entry := range left {
		names = append(names, entry.Name())
	}
	if len(names) != 3 || names[0] != "mid.mp4" || names[1] != "new.mp4" || names[2] != "notes.txt" {
		t.Fatalf("want the oldest recording gone and other files untouched, got %v", names)
	}

	pruneRecordings(dir, 0)
	if left, _ = os.ReadDir(dir); len(left) != 3 {
		t.Fatal("a zero cap must keep everything")
	}
}
