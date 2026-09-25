//go:build !windows

package computer

import (
	"os"
	"syscall"
)

// tryLockFile takes an exclusive, non-blocking flock on f.
func tryLockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
