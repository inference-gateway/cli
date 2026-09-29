package custom

import (
	"os/exec"

	process "github.com/shirou/gopsutil/v4/process"
)

// killProcessTreeOnCancel kills the command and every process it started on
// timeout or cancel, on every platform, so a script's children die too.
// ponytail: a descendant whose parent already exited is reparented out of the
// tree and survives. Job objects or cgroups if that ever shows up.
func killProcessTreeOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		root, err := process.NewProcess(int32(cmd.Process.Pid))
		if err != nil {
			return cmd.Process.Kill()
		}
		return killTree(root)
	}
}

// killTree lists p's children before killing p, so none is reparented out of
// reach, then kills each child's tree the same way.
func killTree(p *process.Process) error {
	children, _ := p.Children()
	err := p.Kill()
	for _, child := range children {
		_ = killTree(child)
	}
	return err
}
