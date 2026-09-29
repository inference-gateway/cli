//go:build !windows

package agui

import (
	"os/exec"
	"syscall"
)

// detachDaemon lets the daemon survive this process exiting: it leaves the
// inherited terminal's foreground group, so a closed terminal cannot take it
// down with a SIGHUP.
func detachDaemon(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
