package projects

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListArtifactsNewestFirstFilesOnly(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.png")
	recent := filepath.Join(dir, "recent.gif")
	_ = os.WriteFile(old, []byte("o"), 0o600)
	_ = os.WriteFile(recent, []byte("rr"), 0o600)
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o750)
	_ = os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))

	listed, err := ListArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Name != "recent.gif" || listed[1].Name != "old.png" || listed[0].Size != 2 {
		t.Fatalf("unexpected listing %+v", listed)
	}
}

func TestListArtifactsMissingDirIsEmpty(t *testing.T) {
	listed, err := ListArtifacts(filepath.Join(t.TempDir(), "none"))
	if err != nil || len(listed) != 0 {
		t.Fatalf("expected an empty listing, got %+v, %v", listed, err)
	}
}

func TestArtifactPathRefusesTraversal(t *testing.T) {
	for _, name := range []string{"", "..", "../x", "a/b", "/etc/passwd"} {
		if _, err := ArtifactPath("/dir", name); err == nil {
			t.Errorf("%q must be refused", name)
		}
	}
	if p, err := ArtifactPath("/dir", "shot.png"); err != nil || p != "/dir/shot.png" {
		t.Fatalf("got %q, %v", p, err)
	}
}
