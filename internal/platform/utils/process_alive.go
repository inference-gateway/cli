package utils

import (
	"math"

	process "github.com/shirou/gopsutil/v4/process"
)

// ProcessAlive reports whether a process with the given PID exists, on every
// platform. A pid that is not positive never names one of ours, even where
// the OS gives pid 0 to a system process.
func ProcessAlive(pid int) bool {
	if pid <= 0 || pid > math.MaxInt32 {
		return false
	}
	alive, err := process.PidExists(int32(pid))
	return err == nil && alive
}
