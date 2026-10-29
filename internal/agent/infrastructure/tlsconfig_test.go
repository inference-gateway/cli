package infrastructure

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestClientForDaemonPinsTheCAFileElseTrustsTheSystemRoots(t *testing.T) {
	pki, err := LoadOrCreatePKI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	issued, err := pki.IssueServer("workstation")
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caFile, issued.CAPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		tls        config.PeerTLSConfig
		wantPinned bool
		wantErr    bool
	}{
		{name: "system roots without a ca_file"},
		{name: "pinned to the ca_file", tls: config.PeerTLSConfig{CAFile: caFile}, wantPinned: true},
		{name: "unreadable ca_file", tls: config.PeerTLSConfig{CAFile: filepath.Join(t.TempDir(), "missing.crt")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := config.EnvironmentEntry{Name: "ws", Kind: config.EnvironmentKindDaemon, Address: "ws.tailnet.ts.net:443", TLS: tt.tls}
			got, err := NewTLSProvider("laptop", nil).ClientForDaemon(entry)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want an error for the unreadable CA file")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if pinned := got.RootCAs != nil; pinned != tt.wantPinned {
				t.Fatalf("pinned = %v, want %v", pinned, tt.wantPinned)
			}
			if got.ServerName != "ws.tailnet.ts.net" || got.MinVersion != tls.VersionTLS13 {
				t.Fatalf("ServerName = %q, MinVersion = %x", got.ServerName, got.MinVersion)
			}
		})
	}
}
