package process

import (
	"context"
	"os/exec"
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
