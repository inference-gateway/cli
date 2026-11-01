package gateway

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
)

func TestTransportReachesAGatewayBehindAPrivateCA(t *testing.T) {
	pki := writeGatewayClientMaterial(t)
	mutual := config.PeerTLSConfig{CAFile: "~/ca.crt", CertFile: "~/client.crt", KeyFile: "~/client.key"}

	tests := []struct {
		name       string
		clientAuth tls.ClientAuthType
		tls        config.PeerTLSConfig
		wantErr    string
	}{
		{name: "the private CA is unknown without a ca_file", wantErr: "failed to verify certificate"},
		{name: "the private CA from ca_file", tls: config.PeerTLSConfig{CAFile: "~/ca.crt"}},
		{name: "mTLS with the client certificate", clientAuth: tls.RequireAndVerifyClientCert, tls: mutual},
		{name: "mTLS without the client certificate", clientAuth: tls.RequireAndVerifyClientCert, tls: config.PeerTLSConfig{CAFile: "~/ca.crt"}, wantErr: "certificate required"},
		{name: "a missing ca_file", tls: config.PeerTLSConfig{CAFile: "~/missing.crt"}, wantErr: "gateway.tls.ca_file"},
		{name: "a cert_file without its key_file", tls: config.PeerTLSConfig{CAFile: "~/ca.crt", CertFile: "~/client.crt"}, wantErr: "gateway.tls.key_file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := startTLSGateway(t, pki, tt.clientAuth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

			resp, err := (&http.Client{Transport: Transport(tt.tls)}).Get(srv.URL)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
		})
	}
}

func TestTransportIsTheDefaultWithoutTLSSettings(t *testing.T) {
	if Transport(config.PeerTLSConfig{}) != http.DefaultTransport {
		t.Fatal("want http.DefaultTransport when gateway.tls is empty")
	}
}

func TestVersionAsksAnMTLSGatewayWithTheClientCertificate(t *testing.T) {
	pki := writeGatewayClientMaterial(t)
	srv := startTLSGateway(t, pki, tls.RequireAndVerifyClientCert, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.61.0"}`))
	}))
	cfg := config.DefaultConfig()
	cfg.Gateway.URL = srv.URL
	cfg.Gateway.Run = false
	cfg.Gateway.TLS = config.PeerTLSConfig{CAFile: "~/ca.crt", CertFile: "~/client.crt", KeyFile: "~/client.key"}

	if got := NewSupervisor("test-session", cfg, nil).Version(t.Context()); got != "0.61.0" {
		t.Fatalf("Version() = %q, want 0.61.0", got)
	}
}

// writeGatewayClientMaterial points HOME at a fresh directory holding a private
// CA and a client certificate it signed, as ~/ca.crt, ~/client.crt and ~/client.key.
func writeGatewayClientMaterial(t *testing.T) *agentinfra.PKI {
	t.Helper()
	pki, err := agentinfra.LoadOrCreatePKI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client, err := pki.IssueClient("infer")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	for name, pem := range map[string][]byte{"ca.crt": pki.CAPEM(), "client.crt": client.CertPEM, "client.key": client.KeyPEM} {
		if err := os.WriteFile(filepath.Join(home, name), pem, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return pki
}

// startTLSGateway serves handler over TLS with a certificate from pki, asking
// for client certificates from the same CA as clientAuth says.
func startTLSGateway(t *testing.T, pki *agentinfra.PKI, clientAuth tls.ClientAuthType, handler http.Handler) *httptest.Server {
	t.Helper()
	issued, err := pki.IssueServer("gateway")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := issued.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, ClientCAs: pki.Pool(), ClientAuth: clientAuth, MinVersion: tls.VersionTLS12}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}
