package infrastructure

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	caValidity   = 10 * 365 * 24 * time.Hour
	leafValidity = 365 * 24 * time.Hour
	caCertFile   = "ca.crt"
	caKeyFile    = "ca.key"
)

// PKI is the daemon-managed certificate authority: one CA per daemon that
// issues server certificates for the workspaces it provisions and a client
// certificate for itself, so a Kubernetes pod and the laptop authenticate
// each other with mTLS and no external authority.
type PKI struct {
	dir  string
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// IssuedCert is one certificate with its key, PEM encoded for files and
// Secrets, plus the CA that signed it.
type IssuedCert struct {
	CertPEM []byte
	KeyPEM  []byte
	CAPEM   []byte
}

// TLSCertificate decodes the issued pair for a tls.Config.
func (c IssuedCert) TLSCertificate() (tls.Certificate, error) {
	return tls.X509KeyPair(c.CertPEM, c.KeyPEM)
}

// LoadOrCreatePKI opens the CA under dir, minting it on first use with files
// only the user can read.
func LoadOrCreatePKI(dir string) (*PKI, error) {
	certPEM, certErr := os.ReadFile(filepath.Join(dir, caCertFile))
	keyPEM, keyErr := os.ReadFile(filepath.Join(dir, caKeyFile))
	if certErr == nil && keyErr == nil {
		return loadPKI(dir, certPEM, keyPEM)
	}
	if !errors.Is(certErr, os.ErrNotExist) && certErr != nil {
		return nil, fmt.Errorf("reading the daemon CA: %w", certErr)
	}
	return createPKI(dir)
}

func loadPKI(dir string, certPEM, keyPEM []byte) (*PKI, error) {
	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certBlock == nil || keyBlock == nil {
		return nil, errors.New("the daemon CA files are not PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing the daemon CA certificate: %w", err)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing the daemon CA key: %w", err)
	}
	return &PKI{dir: dir, cert: cert, key: key}, nil
}

func createPKI(dir string) (*PKI, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "infer daemon CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(caValidity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating the daemon CA: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating the daemon CA directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, caCertFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, caKeyFile), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		return nil, err
	}
	return &PKI{dir: dir, cert: cert, key: key}, nil
}

// CAPEM is the CA certificate peers pin.
func (p *PKI) CAPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.cert.Raw})
}

// Pool is the CA as a cert pool for verifying peers.
func (p *PKI) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(p.cert)
	return pool
}

// IssueServer issues a server certificate for name, valid for the given DNS
// names and IPs, loopback included so a port-forward verifies.
func (p *PKI) IssueServer(name string, hosts ...string) (IssuedCert, error) {
	template := leafTemplate(name, x509.ExtKeyUsageServerAuth)
	template.DNSNames = append(template.DNSNames, "localhost")
	template.IPAddresses = append(template.IPAddresses, net.IPv4(127, 0, 0, 1))
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
			continue
		}
		template.DNSNames = append(template.DNSNames, host)
	}
	return p.issue(template)
}

// IssueClient issues a client certificate naming this daemon.
func (p *PKI) IssueClient(name string) (IssuedCert, error) {
	return p.issue(leafTemplate(name, x509.ExtKeyUsageClientAuth))
}

func leafTemplate(name string, usage x509.ExtKeyUsage) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
}

func (p *PKI) issue(template *x509.Certificate) (IssuedCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return IssuedCert{}, err
	}
	der, err := x509.CreateCertificate(rand.Reader, template, p.cert, &key.PublicKey, p.key)
	if err != nil {
		return IssuedCert{}, fmt.Errorf("issuing a certificate for %s: %w", template.Subject.CommonName, err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return IssuedCert{}, err
	}
	return IssuedCert{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		CAPEM:   p.CAPEM(),
	}, nil
}

func serial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}
