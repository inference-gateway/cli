//go:build !windows

package tools

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAcquireScreenLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	screenLockMu.Lock()
	saved := screenLockFile
	screenLockFile = nil
	screenLockMu.Unlock()
	defer func() {
		screenLockMu.Lock()
		if screenLockFile != nil {
			_ = screenLockFile.Close()
		}
		screenLockFile = saved
		screenLockMu.Unlock()
	}()

	if err := acquireScreenLock(); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if err := acquireScreenLock(); err != nil {
		t.Fatalf("re-acquire in the same process failed: %v", err)
	}

	home, _ := os.UserHomeDir()
	f, err := os.OpenFile(filepath.Join(home, ".infer", "run", "computer-use.lock"), os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("failed to open lock file: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("second flock on a held lock unexpectedly succeeded")
	}
}
