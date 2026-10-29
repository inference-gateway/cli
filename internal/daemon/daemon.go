package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	process "github.com/inference-gateway/cli/internal/platform/process"
)

// bootWait bounds how long EnsureRunning waits for a daemon it just started to
// bind its port.
const bootWait = 15 * time.Second

// EnsureRunning makes an infer daemon serve its binding on port, starting one
// in the background when nothing listens there. The daemon's own pid lock makes
// a redundant start exit, so callers never coordinate, and the wait covers a
// daemon already on its way.
func EnsureRunning(ctx context.Context, port int) error {
	if reachable(port) {
		return nil
	}
	if err := start(); err != nil {
		return fmt.Errorf("starting the infer daemon failed: %w", err)
	}
	deadline := time.Now().Add(bootWait)
	for {
		if reachable(port) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the infer daemon did not open its binding on port %d in time (an infer daemon already running without its binding has to be restarted)", port)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// autoStartIdleExit is how long a daemon started on demand idles before it
// stops, unless daemon.yaml sets its own idle_exit.
const autoStartIdleExit = 30 * time.Minute

// start runs this binary as a background daemon in a session of its own, so
// it and the threads it runs outlive the caller's terminal, and it stops once
// idle for autoStartIdleExit. Its callers dial its binding, so it serves one
// whatever daemon.yaml says. A package var, so tests stub the boot. It
// inherits the caller's env and working directory, so its config.
var start = func() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := process.Command(exe, "daemon", "--idle-exit", autoStartIdleExit.String())
	cmd.Env = append(os.Environ(), "INFER_DAEMON_BINDING_ENABLED=true")
	process.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	logger.Info("started the infer daemon", "pid", cmd.Process.Pid)
	return cmd.Process.Release()
}

// reachable reports whether something accepts TCP on port.
func reachable(port int) bool {
	conn, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).
		Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
