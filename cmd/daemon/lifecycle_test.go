package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWatchExecutableNoticesTheBinaryIsGone(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "infer")
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone := watchExecutable(t.Context(), exe)

	select {
	case <-gone:
		t.Fatal("the daemon stopped while its binary was still there")
	case <-time.After(2 * executableCheck):
	}

	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gone:
	case <-time.After(5 * executableCheck):
		t.Fatal("the daemon kept running after its binary was removed")
	}
}

func TestDaemonLockAdmitsOneDaemonAndOutlivesNoHolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	pidPath := filepath.Join(home, ".infer", "run", "daemon.pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	release, err := acquireDaemonLock()
	if err != nil {
		t.Fatalf("a file a crashed daemon left was not taken over: %v", err)
	}
	if pid, _ := os.ReadFile(pidPath); string(pid) != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid file = %q, want this daemon's pid", pid)
	}
	if _, err := acquireDaemonLock(); err == nil || !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("a second daemon = %v, want refused naming the running one", err)
	}

	release()
	again, err := acquireDaemonLock()
	if err != nil {
		t.Fatalf("the lock outlived its holder: %v", err)
	}
	again()
}
