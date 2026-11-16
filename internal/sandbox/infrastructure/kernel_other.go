//go:build !linux && !darwin

package sandboxinfra

import (
	"context"

	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

const (
	kernelSupported = false
	kernelName      = ""
)

func kernelDenied(_ []sandboxdomain.Denied) []sandboxdomain.Denied {
	return nil
}

func confinedChild(ctx context.Context, _ []string, _ []sandboxdomain.Denied, command string) *Child {
	return Unconfined(ctx, command)
}

// start is a plain start: this platform has no kernel sandbox.
func (c *Child) start() error {
	return c.Cmd.Start()
}
