package services

// ProcessAlive reports whether a process with the given PID exists.
func ProcessAlive(pid int) bool {
	return processAlive(pid)
}
