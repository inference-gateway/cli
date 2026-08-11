//go:build unix

package services

import "syscall"

// processAlive reports whether a process with the given PID exists, using the
// portable signal-0 probe.
func processAlive(pid int) bool {
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}
