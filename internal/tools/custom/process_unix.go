//go:build unix

package custom

import (
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel starts the command in its own process group and
// kills the whole group on timeout or cancel, so a script's children die too.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
