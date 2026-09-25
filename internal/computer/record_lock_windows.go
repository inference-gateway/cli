//go:build windows

package computer

import (
	"os"

	windows "golang.org/x/sys/windows"
)

// tryLockFile takes an exclusive, non-blocking lock on the first byte of f.
func tryLockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
