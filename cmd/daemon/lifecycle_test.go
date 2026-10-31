package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	config "github.com/inference-gateway/cli/config"
	sessions "github.com/inference-gateway/cli/internal/sessions"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
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

func TestWatchIdleExitsSoonOnceEverySessionTeleportedAway(t *testing.T) {
	away := sessionsdomain.ThreadKey{ProjectDir: "/workspace/x/conv-1", ConversationID: "conv-1"}
	staying := sessionsdomain.ThreadKey{ProjectDir: "/workspace/y/conv-2", ConversationID: "conv-2"}
	tests := []struct {
		name       string
		awayExit   time.Duration
		here       []sessionsdomain.ThreadKey
		client     bool
		wantSoon   bool
		wantClosed bool
	}{
		{name: "the last session teleported away", awayExit: time.Minute, wantSoon: true, wantClosed: true},
		{name: "a client is attached", awayExit: time.Minute, client: true},
		{name: "another session is still here", awayExit: time.Minute, here: []sessionsdomain.ThreadKey{staying}, wantClosed: true},
		{name: "away_exit is off", wantClosed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				registry := sessions.NewRegistry(nil, time.Minute)
				for _, key := range tt.here {
					registry.TeleportedHere(key)
				}
				registry.TeleportedHere(away)
				registry.TeleportedAway(away)
				if tt.client {
					registry.Attach(&sessionsmocks.FakeClient{})
				}
				var reported []bool
				daemon := config.DaemonConfig{IdleExit: 2 * time.Hour, AwayExit: tt.awayExit}
				idle := watchIdle(ctx, daemon, registry, func(done bool) error {
					reported = append(reported, done)
					return nil
				})

				time.Sleep(time.Minute + idleCheck)
				synctest.Wait()
				if closed(idle) != tt.wantSoon {
					t.Fatalf("idled out a minute later = %v, want %v", closed(idle), tt.wantSoon)
				}
				time.Sleep(daemon.IdleExit)
				synctest.Wait()
				if closed(idle) != tt.wantClosed {
					t.Fatalf("idled out after idle_exit = %v, want %v", closed(idle), tt.wantClosed)
				}
				if tt.wantSoon && (len(reported) != 1 || !reported[0]) {
					t.Fatalf("reported %v, want agents done once", reported)
				}
			})
		})
	}
}

func closed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}
