package infrastructure

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestPKIIssuesVerifiablePairsAndPersists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pki")
	pki, err := LoadOrCreatePKI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "ca.key")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("ca.key mode = %v, %v", info, err)
	}

	server, err := pki.IssueServer("infer-abc", "10.0.0.5", "infer-abc.ns.svc")
	if err != nil {
		t.Fatal(err)
	}
	client, err := pki.IssueClient("laptop")
	if err != nil {
		t.Fatal(err)
	}

	serverCert, err := server.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(serverCert.Certificate[0])
	for _, host := range []string{"infer-abc", "localhost", "127.0.0.1", "10.0.0.5", "infer-abc.ns.svc"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: pki.Pool(), DNSName: host}); err != nil {
			t.Fatalf("server cert does not verify for %s: %v", host, err)
		}
	}
	clientCert, _ := client.TLSCertificate()
	clientLeaf, _ := x509.ParseCertificate(clientCert.Certificate[0])
	if _, err := clientLeaf.Verify(x509.VerifyOptions{Roots: pki.Pool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("client cert does not verify: %v", err)
	}

	reloaded, err := LoadOrCreatePKI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.cert.Equal(pki.cert) {
		t.Fatal("reloading the PKI minted a new CA")
	}
}

func TestSealedEnvOpensOnlyForTheRightKeyAndTeleport(t *testing.T) {
	pki, err := LoadOrCreatePKI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	issued, _ := pki.IssueServer("pod")
	cert, _ := issued.TLSCertificate()
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])

	env := map[string]string{"GITHUB_TOKEN": "ghp_secret", "INFER_GATEWAY_API_KEY": "k"}
	sealed, err := SealEnv(leaf, env, "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == "" || containsAll(sealed, "ghp_secret") {
		t.Fatal("the sealed blob leaks the plaintext")
	}

	ring := &Keyring{}
	if _, err := ring.Open(sealed, "t-1"); err == nil {
		t.Fatal("an empty keyring opened the blob")
	}
	ring.Remember(&cert)
	opened, err := ring.Open(sealed, "t-1")
	if err != nil || opened["GITHUB_TOKEN"] != "ghp_secret" || len(opened) != 2 {
		t.Fatalf("Open = %v, %v", opened, err)
	}
	if _, err := ring.Open(sealed, "t-2"); err == nil {
		t.Fatal("a blob opened under another teleport id")
	}

	other, _ := pki.IssueServer("other")
	otherCert, _ := other.TLSCertificate()
	if _, err := OpenEnv(otherCert.PrivateKey.(*ecdsa.PrivateKey), sealed, "t-1"); err == nil {
		t.Fatal("another key opened the blob")
	}

	empty, err := SealEnv(leaf, nil, "t-1")
	if err != nil || empty != nil {
		t.Fatalf("an empty env seals to %v, %v", empty, err)
	}
}

func TestKeyringKeepsTheNewestFour(t *testing.T) {
	pki, _ := LoadOrCreatePKI(t.TempDir())
	ring := &Keyring{}
	var certs []tls.Certificate
	for range 6 {
		issued, _ := pki.IssueServer("pod")
		cert, _ := issued.TLSCertificate()
		certs = append(certs, cert)
		ring.Remember(&cert)
	}
	ring.Remember(&certs[5])
	if len(ring.keys) != keyringSize {
		t.Fatalf("keyring holds %d keys, want %d", len(ring.keys), keyringSize)
	}
	oldest, _ := x509.ParseCertificate(certs[0].Certificate[0])
	sealed, _ := SealEnv(oldest, map[string]string{"A": "b"}, "t")
	if _, err := ring.Open(sealed, "t"); err == nil {
		t.Fatal("a key older than the ring still opens blobs")
	}
}

func containsAll(data []byte, needle string) bool {
	return len(needle) > 0 && string(data) != "" && indexOf(string(data), needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
