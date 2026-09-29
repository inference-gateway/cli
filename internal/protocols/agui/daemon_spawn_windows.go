//go:build windows

package agui

import (
	"os/exec"
)

// detachDaemon leaves the default process group on Windows, where a child
// already outlives its parent's console.
func detachDaemon(_ *exec.Cmd) {}
