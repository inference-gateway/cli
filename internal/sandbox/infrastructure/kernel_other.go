//go:build !linux && !darwin

package sandboxinfra

import "context"

const kernelSupported = false

func confinedChild(ctx context.Context, _ []string, command string) *Child {
	return Unconfined(ctx, command)
}

// start is a plain start: this platform has no kernel sandbox.
func (c *Child) start() error {
	return c.Cmd.Start()
}
