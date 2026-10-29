package process

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLockFileHoldsTheFileUntilItsHolderLetsGo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	held, err := LockFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := LockFile(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("a second lock = %v, want ErrLocked", err)
	}

	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := LockFile(path)
	if err != nil {
		t.Fatalf("locking the released file: %v", err)
	}
	_ = again.Close()
}
