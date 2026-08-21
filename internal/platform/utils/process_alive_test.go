package utils

import (
	"os"
	"os/exec"
	"testing"
)

func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Fatal("ProcessAlive(self) = false, want true")
	}

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sleep: %v", err)
	}
	pid := cmd.Process.Pid
	if !ProcessAlive(pid) {
		t.Fatalf("ProcessAlive(%d) = false while running, want true", pid)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("killing sleep: %v", err)
	}
	_ = cmd.Wait()
	if ProcessAlive(pid) {
		t.Fatalf("ProcessAlive(%d) = true after reap, want false", pid)
	}
}
