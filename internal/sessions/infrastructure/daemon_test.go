package infrastructure

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestEnsureDaemon(t *testing.T) {
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
			previous := startDaemon
			startDaemon = func() error { started = true; return errors.New("permission denied") }
			t.Cleanup(func() { startDaemon = previous })

			err = EnsureDaemon(t.Context(), port)
			if tt.wantErr == "" {
				if err != nil || started {
					t.Fatalf("EnsureDaemon = %v, started = %v, want nil without a start", err, started)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("EnsureDaemon = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
