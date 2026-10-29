//go:build windows

package process

import (
	"errors"
	"os"

	windows "golang.org/x/sys/windows"
)

// lockFile locks one byte far past the content, since a locked range cannot be
// read by others and the content stays readable this way.
func lockFile(file *os.File) error {
	region := &windows.Overlapped{OffsetHigh: 0x7fffffff}
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, region)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocked
	}
	return err
}
