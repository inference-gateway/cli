package process

import (
	"context"
	"errors"
	"os"
	"os/exec"

	psutil "github.com/shirou/gopsutil/v4/process"
)

// Command is exec.Command for a child that must never open its own console
// window. On windows a console child of a parent without a console gets a
// fresh window, so infer running under a GUI host flashes one per spawn.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	hideConsole(cmd)
	return cmd
}

// CommandContext is exec.CommandContext with the same console suppression
// as Command.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hideConsole(cmd)
	return cmd
}

// ErrLocked is LockFile's answer while another holder has the file locked.
var ErrLocked = errors.New("locked by another process")

// LockFile opens the file at path, creating it, and locks it for this process
// without waiting, or returns ErrLocked. Closing the file releases the lock,
// and so does the process exiting, however it exits, so a lock never outlives
// its holder.
func LockFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// Detach makes cmd's process outlive the caller's terminal: it starts in a
// session (a process group on windows) of its own, so closing the terminal or
// its Ctrl+C never reaches it.
func Detach(cmd *exec.Cmd) {
	detach(cmd)
}

// KillTreeOnCancel kills the command and every process it started on timeout
// or cancel, on every platform, so a script's children die too and stop
// holding its output pipes open. A descendant whose parent already exited is
// reparented out of the tree and survives: job objects or cgroups if that
// ever shows up.
func KillTreeOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		root, err := psutil.NewProcess(int32(cmd.Process.Pid))
		if err != nil {
			return cmd.Process.Kill()
		}
		return killTree(root)
	}
}

// killTree lists p's children before killing p, so none is reparented out of
// reach, then kills each child's tree the same way.
func killTree(p *psutil.Process) error {
	children, _ := p.Children()
	err := p.Kill()
	for _, child := range children {
		_ = killTree(child)
	}
	return err
}
