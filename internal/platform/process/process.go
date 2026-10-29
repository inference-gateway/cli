package process

import (
	"context"
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
