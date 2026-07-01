//go:build unix

package memory

import (
	"os"
	"path/filepath"
	"syscall"
)

// lockDir takes a best-effort advisory exclusive lock scoped to the memory dir
// so concurrent infer subprocesses on one host (channels/scheduler/heartbeat)
// serialize their git sync. The returned func releases the lock and is always
// safe to call. Any failure degrades to no locking - git's own index.lock and
// the push-then-rebase retry remain as safety nets.
func lockDir(dir string) func() {
	path := lockPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}
