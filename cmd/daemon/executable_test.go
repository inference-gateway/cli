package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchExecutableNoticesTheBinaryIsGone(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "infer")
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone := watchExecutable(t.Context(), exe)

	select {
	case <-gone:
		t.Fatal("the daemon stopped while its binary was still there")
	case <-time.After(2 * executableCheck):
	}

	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gone:
	case <-time.After(5 * executableCheck):
		t.Fatal("the daemon kept running after its binary was removed")
	}
}
