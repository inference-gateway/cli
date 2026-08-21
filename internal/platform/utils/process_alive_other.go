//go:build !unix

package utils

import "os"

// ProcessAlive reports whether a process with the given PID exists. On Windows
// os.FindProcess opens a handle to the process and fails if it does not exist.
func ProcessAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
