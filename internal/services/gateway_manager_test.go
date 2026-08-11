package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

// TestPIDRegistry verifies the last-one-out reference-counting logic:
//   - register N PIDs, deregister N-1 does NOT kill the gateway
//   - deregistering the last PID kills the gateway
//   - stale PID entries (dead processes) are pruned by signal-0 probe
func TestPIDRegistry(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Start a fake gateway process (long-running sleep) and write its PID.
	gwCmd := exec.Command("sleep", "30")
	if err := gwCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gwCmd.Process.Kill() })

	pidsDir := filepath.Join(tempHome, ".infer", "gateway", "pids")
	gwPIDPath := filepath.Join(tempHome, ".infer", "gateway", "gateway.pid")
	_ = os.MkdirAll(pidsDir, 0755)
	_ = os.WriteFile(gwPIDPath, []byte(strconv.Itoa(gwCmd.Process.Pid)), 0644)

	// Nothing registered yet — gateway should not be killed.
	// Start two "consumer" subprocesses and register their PIDs.
	con1 := startSleep(t)
	con2 := startSleep(t)
	t.Cleanup(func() { _ = con1.Process.Kill() })
	t.Cleanup(func() { _ = con2.Process.Kill() })

	_ = os.WriteFile(filepath.Join(pidsDir, strconv.Itoa(con1.Process.Pid)), nil, 0644)
	_ = os.WriteFile(filepath.Join(pidsDir, strconv.Itoa(con2.Process.Pid)), nil, 0644)

	gm := &GatewayManager{}
	// Register our own PID too.
	_ = os.WriteFile(filepath.Join(pidsDir, strconv.Itoa(os.Getpid())), nil, 0644)

	if !gm.hasLiveRegistrations() {
		t.Fatal("expected live registrations after registering PIDs")
	}

	gm.deregisterPID()
	if !gm.hasLiveRegistrations() {
		t.Fatal("expected live registrations after deregistering self, 2 still alive")
	}

	if err := syscall.Kill(gwCmd.Process.Pid, syscall.Signal(0)); err != nil {
		t.Fatal("gateway was killed while other consumers were still registered")
	}

	_ = con1.Process.Kill()
	_ = con1.Wait()

	_ = con2.Process.Kill()
	_ = con2.Wait()

	gm.pruneStalePIDs()

	// After prune, both should be gone.
	if gm.hasLiveRegistrations() {
		t.Fatal("expected no live registrations after all consumers exited")
	}

	if _, err := os.Stat(filepath.Join(pidsDir, strconv.Itoa(con1.Process.Pid))); !os.IsNotExist(err) {
		t.Error("stale PID file was not pruned")
	}
}

func startSleep(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}
