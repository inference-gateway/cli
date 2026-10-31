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
	home := t.TempDir()
	t.Setenv("HOME", home)
	caFile := filepath.Join(home, "ca.crt")
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
		{name: "pinned to a ca_file under ~", tls: config.PeerTLSConfig{CAFile: "~/ca.crt"}, wantPinned: true},
		{name: "unreadable ca_file", tls: config.PeerTLSConfig{CAFile: filepath.Join(t.TempDir(), "missing.crt")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := config.WorkspaceEntry{Name: "ws", Kind: config.WorkspaceKindDaemon, Address: "ws.tailnet.ts.net:443", TLS: tt.tls}
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

func TestServerLoadsTheCertificateFilesUnderHome(t *testing.T) {
	pki, err := LoadOrCreatePKI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	issued, err := pki.IssueServer("workstation")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	for name, pem := range map[string][]byte{"tls.crt": issued.CertPEM, "tls.key": issued.KeyPEM, "ca.crt": issued.CAPEM} {
		if err := os.WriteFile(filepath.Join(home, name), pem, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		tls  config.BindingTLSConfig
	}{
		{name: "absolute paths", tls: config.BindingTLSConfig{Mode: config.TLSModeFiles, CertFile: filepath.Join(home, "tls.crt"), KeyFile: filepath.Join(home, "tls.key")}},
		{name: "paths under ~", tls: config.BindingTLSConfig{Mode: config.TLSModeFiles, CertFile: "~/tls.crt", KeyFile: "~/tls.key"}},
		{name: "client auth with a ca_file under ~", tls: config.BindingTLSConfig{Mode: config.TLSModeFiles, CertFile: "~/tls.crt", KeyFile: "~/tls.key", CAFile: "~/ca.crt", ClientAuth: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTLSProvider("workstation", nil).Server(tt.tls)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Certificates) != 1 {
				t.Fatalf("Certificates = %d, want 1", len(got.Certificates))
			}
			if tt.tls.ClientAuth && got.ClientCAs == nil {
				t.Fatal("ClientCAs = nil, want the ca_file pool")
			}
		})
	}
}
