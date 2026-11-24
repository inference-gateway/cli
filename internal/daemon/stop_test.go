package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	process "github.com/inference-gateway/cli/internal/platform/process"
)

func TestStopStopsOnlyTheDaemonHoldingThePidFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a sleep process stands in for the daemon")
	}
	tests := []struct {
		name     string
		pidFile  bool
		locked   bool
		wantStop bool
	}{
		{"nothing runs without a pid file", false, false, false},
		{"a pid file no daemon holds may name a process that reused the pid", true, false, false},
		{"the daemon holding the pid file stops", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			standIn, exited := startSleeper(t)
			if tt.pidFile {
				writePIDFile(t, standIn, tt.locked)
			}

			pid, err := Stop(t.Context())
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}

			if !tt.wantStop {
				if pid != 0 {
					t.Fatalf("Stop = pid %d, want 0", pid)
				}
				select {
				case <-exited:
					t.Fatal("a process no daemon lock vouches for was stopped")
				case <-time.After(200 * time.Millisecond):
				}
				return
			}
			if pid != standIn {
				t.Fatalf("Stop = pid %d, want %d", pid, standIn)
			}
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				t.Fatal("the daemon still runs")
			}
		})
	}
}

func startSleeper(t *testing.T) (int, <-chan struct{}) {
	t.Helper()
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = sleeper.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
		<-exited
	})
	return sleeper.Process.Pid, exited
}

func writePIDFile(t *testing.T, pid int, locked bool) {
	t.Helper()
	path := PIDFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	if !locked {
		return
	}
	lock, err := process.LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
}
