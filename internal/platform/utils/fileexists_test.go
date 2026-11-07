package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists(t *testing.T) {
	t.Run("a created path exists", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "present.txt")
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatalf("writing the file: %v", err)
		}
		if !FileExists(path) {
			t.Errorf("FileExists(%q) = false, want true", path)
		}
	})

	t.Run("a missing path does not", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.txt")
		if FileExists(path) {
			t.Errorf("FileExists(%q) = true, want false", path)
		}
	})
}
