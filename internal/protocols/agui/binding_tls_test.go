package agui_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"

	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// testCA issues the server and client certificates one TLS test needs.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

func (ca *testCA) issue(t *testing.T, name string, usage x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func (ca *testCA) serverConfig(t *testing.T, clientAuth tls.ClientAuthType) *tls.Config {
	t.Helper()
	return &tls.Config{
		Certificates: []tls.Certificate{ca.issue(t, "localhost", x509.ExtKeyUsageServerAuth)},
		ClientAuth:   clientAuth,
		ClientCAs:    ca.pool,
		MinVersion:   tls.VersionTLS13,
	}
}

func (ca *testCA) clientConfig(t *testing.T, withCert bool) *tls.Config {
	t.Helper()
	cfg := &tls.Config{RootCAs: ca.pool, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	if withCert {
		cfg.Certificates = []tls.Certificate{ca.issue(t, "peer", x509.ExtKeyUsageClientAuth)}
	}
	return cfg
}

func dialBinding(t *testing.T, binding *agui.Binding, cfg agui.DialConfig) (*agui.Conn, *aguimocks.FakeHandler, error) {
	t.Helper()
	cfg.Addr = binding.Addr()
	cfg.Handshake = testHandshake
	handler := &aguimocks.FakeHandler{}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, err := agui.Dial(ctx, cfg, handler)
	if conn != nil {
		t.Cleanup(conn.Close)
	}
	return conn, handler, err
}

func TestBindingServesTLSAndTheDialerReadsTheAck(t *testing.T) {
	ca := newTestCA(t)
	binding, server := startBinding(t, agui.BindingConfig{
		Host:      "127.0.0.1",
		Token:     "test-token",
		TLS:       ca.serverConfig(t, tls.NoClientCert),
		AckAttrs:  map[string]any{"workspace": "workstation", "storage_id": "abc"},
		Handshake: testHandshake,
	})

	conn, _, err := dialBinding(t, binding, agui.DialConfig{Token: "test-token", Kind: "daemon", TLS: ca.clientConfig(t, false)})
	if err != nil {
		t.Fatalf("dial over TLS: %v", err)
	}
	if got := conn.AckAttr("workspace"); got != "workstation" {
		t.Fatalf("workspace ack attr = %q, want workstation", got)
	}
	if got := conn.AckAttr("storage_id"); got != "abc" {
		t.Fatalf("storage_id ack attr = %q, want abc", got)
	}
	if got := conn.AckAttr("protocol_version"); got == "" {
		t.Fatal("the protocol version is not an ack attr")
	}
	if len(conn.PeerCertificates()) == 0 {
		t.Fatal("the dialer sees no server certificate")
	}
	eventually(t, "the connection to attach", func() bool { return server.AttachCallCount() == 1 })
	if kind := server.AttachArgsForCall(0).Kind(); kind != "daemon" {
		t.Fatalf("server saw kind %q, want daemon", kind)
	}
}

func TestBindingClientCertificatesStandInForTheToken(t *testing.T) {
	ca := newTestCA(t)
	binding, server := startBinding(t, agui.BindingConfig{
		TLS:       ca.serverConfig(t, tls.RequireAndVerifyClientCert),
		Handshake: testHandshake,
	})

	if _, _, err := dialBinding(t, binding, agui.DialConfig{Kind: "daemon", TLS: ca.clientConfig(t, true)}); err != nil {
		t.Fatalf("dial with a client certificate and no token: %v", err)
	}
	eventually(t, "the connection to attach", func() bool { return server.AttachCallCount() == 1 })
	if len(server.AttachArgsForCall(0).PeerCertificates()) == 0 {
		t.Fatal("the server sees no client certificate")
	}

	if _, _, err := dialBinding(t, binding, agui.DialConfig{Kind: "daemon", TLS: ca.clientConfig(t, false)}); err == nil {
		t.Fatal("a dial without a client certificate was accepted")
	}
}

func TestBindingStartNeedsATokenOrClientCertificates(t *testing.T) {
	ca := newTestCA(t)
	tests := []struct {
		name    string
		cfg     agui.BindingConfig
		wantErr bool
	}{
		{"no token, no TLS", agui.BindingConfig{Handshake: testHandshake}, true},
		{"no token, TLS without client auth", agui.BindingConfig{TLS: ca.serverConfig(t, tls.NoClientCert), Handshake: testHandshake}, true},
		{"no token, mTLS", agui.BindingConfig{TLS: ca.serverConfig(t, tls.RequireAndVerifyClientCert), Handshake: testHandshake}, false},
		{"token, no TLS", agui.BindingConfig{Token: "t", Handshake: testHandshake}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := agui.NewBinding(tt.cfg, &aguimocks.FakeHandler{})
			err := binding.Start()
			if err == nil {
				binding.Close()
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("Start err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBindingOverTLSStillChecksTheToken(t *testing.T) {
	ca := newTestCA(t)
	binding, _ := startBinding(t, agui.BindingConfig{
		Token:     "test-token",
		TLS:       ca.serverConfig(t, tls.NoClientCert),
		Handshake: testHandshake,
	})
	if _, _, err := dialBinding(t, binding, agui.DialConfig{Token: "wrong", Kind: "daemon", TLS: ca.clientConfig(t, false)}); err == nil {
		t.Fatal("a wrong token over TLS was accepted")
	}
}

func TestSnapshotDigestIgnoresMessageIDs(t *testing.T) {
	first := []agui.Message{
		{ID: "a", Role: "user", Content: "hello"},
		{ID: "b", Role: "assistant", ToolCalls: []agui.ToolCall{{ID: "call-1", Type: "function", Function: agui.FunctionCall{Name: "Read", Arguments: "{}"}}}},
		{ID: "c", Role: "tool", ToolCallID: "call-1", Content: "done"},
	}
	second := []agui.Message{
		{ID: "x", Role: "user", Content: "hello"},
		{ID: "y", Role: "assistant", ToolCalls: []agui.ToolCall{{ID: "call-1", Type: "function", Function: agui.FunctionCall{Name: "Read", Arguments: "{}"}}}},
		{ID: "z", Role: "tool", ToolCallID: "call-1", Content: "done"},
	}
	if agui.SnapshotDigest(first) != agui.SnapshotDigest(second) {
		t.Fatal("fresh message ids changed the digest")
	}
	second[2].Content = "failed"
	if agui.SnapshotDigest(first) == agui.SnapshotDigest(second) {
		t.Fatal("a changed message kept the digest")
	}
	if agui.SnapshotDigest(nil) == "" {
		t.Fatal("an empty snapshot has no digest")
	}
}

func TestBindingDropsOnlyTheReadinessProbeHandshakeEOF(t *testing.T) {
	tests := []struct {
		name string
		line string
		kept bool
	}{
		{"probe closes before the handshake", "http: TLS handshake error from 10.42.1.1:44590: EOF\n", false},
		{"client presents a bad certificate", "http: TLS handshake error from 10.42.1.1:44590: remote error: tls: bad certificate\n", true},
		{"any other server error", "http: Accept error: too many open files\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			n, err := agui.NewQuietProbes(&out).Write([]byte(tt.line))
			if err != nil || n != len(tt.line) {
				t.Fatalf("Write = %d, %v, want %d, nil", n, err, len(tt.line))
			}
			if kept := out.Len() > 0; kept != tt.kept {
				t.Fatalf("kept = %v, want %v", kept, tt.kept)
			}
		})
	}
}
