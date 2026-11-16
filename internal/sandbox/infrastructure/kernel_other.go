//go:build !linux && !darwin

package sandboxinfra

import (
	"context"
	"errors"
	"os/exec"

	process "github.com/inference-gateway/cli/internal/platform/process"
)

const kernelSupported = false

func confinedCommand(ctx context.Context, _ []string, command string) *exec.Cmd {
	return process.CommandContext(ctx, "bash", "-c", command)
}

// Restrict is the Linux re-exec step. This platform has no kernel sandbox.
func Restrict([]string) error {
	return errors.New("the kernel sandbox is not available on this platform")
}
