package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// daemonBootWait bounds how long EnsureDaemon waits for a daemon it just
// started to bind its port.
const daemonBootWait = 15 * time.Second

// EnsureDaemon makes an infer daemon listen on port, starting one in the
// background when nothing does. The daemon's own pid lock makes a redundant start exit, so
// callers never coordinate, and the wait covers a daemon already on its way.
func EnsureDaemon(ctx context.Context, port int) error {
	if daemonReachable(port) {
		return nil
	}
	if err := startDaemon(); err != nil {
		return fmt.Errorf("starting the infer daemon failed: %w", err)
	}
	deadline := time.Now().Add(daemonBootWait)
	for {
		if daemonReachable(port) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("the infer daemon did not bind its binding port in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// startDaemon runs this binary as a background daemon that outlives the caller.
// A package var, so tests stub the boot instead of spawning the test binary.
// ponytail: no new session, so closing the caller's terminal stops the daemon
// too and the next Browser call starts another. Run it as a service if it must
// survive that. It inherits the caller's env and working directory, so its config.
var startDaemon = func() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon")
	if err := cmd.Start(); err != nil {
		return err
	}
	logger.Info("started the infer daemon", "pid", cmd.Process.Pid)
	return cmd.Process.Release()
}

// daemonReachable reports whether something accepts TCP on port.
func daemonReachable(port int) bool {
	conn, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).
		Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
