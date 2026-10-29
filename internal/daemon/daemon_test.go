package daemon

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestEnsureRunning(t *testing.T) {
	tests := []struct {
		name      string
		listening bool
		wantErr   string
	}{
		{"a listening daemon is left alone", true, ""},
		{"a failed start names the cause", false, "starting the infer daemon failed: permission denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			port := l.Addr().(*net.TCPAddr).Port
			if tt.listening {
				t.Cleanup(func() { _ = l.Close() })
			} else {
				_ = l.Close()
			}
			started := false
			previous := start
			start = func() error { started = true; return errors.New("permission denied") }
			t.Cleanup(func() { start = previous })

			err = EnsureRunning(t.Context(), port)
			if tt.wantErr == "" {
				if err != nil || started {
					t.Fatalf("EnsureRunning = %v, started = %v, want nil without a start", err, started)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("EnsureRunning = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureRunningRetriesWhileAnOldDaemonHoldsTheLock(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	starts := 0
	previous := start
	start = func() error {
		starts++
		if starts < 2 {
			return nil
		}
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return err
		}
		t.Cleanup(func() { _ = listener.Close() })
		return nil
	}
	t.Cleanup(func() { start = previous })

	if err := EnsureRunning(t.Context(), port); err != nil {
		t.Fatalf("EnsureRunning = %v, want the second start to come up", err)
	}
	if starts != 2 {
		t.Errorf("starts = %d, want a retry after the first lost to the old daemon", starts)
	}
}
