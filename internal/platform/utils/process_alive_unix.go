//go:build unix

package utils

import "syscall"

// ProcessAlive reports whether a process with the given PID exists, using the
// portable signal-0 probe.
func ProcessAlive(pid int) bool {
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}
