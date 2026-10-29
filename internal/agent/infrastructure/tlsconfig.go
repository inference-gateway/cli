package infrastructure

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"

	config "github.com/inference-gateway/cli/config"
)

// TLSProvider builds every tls.Config the daemon needs from daemon.yaml: the
// binding's server side and the client side for each environment, from
// certificate files or the daemon-managed CA.
type TLSProvider struct {
	identity string
	pki      *PKI
	ring     *Keyring
}

// NewTLSProvider wires the provider over the daemon's identity and PKI.
func NewTLSProvider(identity string, pki *PKI) *TLSProvider {
	return &TLSProvider{identity: identity, pki: pki, ring: &Keyring{}}
}

// Keyring holds the keys of the certificates the binding served, for opening
// credentials sealed to them.
func (p *TLSProvider) Keyring() *Keyring {
	return p.ring
}

// Server builds the binding's TLS config for its mode, nil for off.
func (p *TLSProvider) Server(cfg config.BindingTLSConfig) (*tls.Config, error) {
	var tlsConfig *tls.Config
	switch cfg.Mode {
	case "", config.TLSModeOff:
		return nil, nil
	case config.TLSModeFiles:
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading the binding certificate: %w", err)
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
		if cfg.RequiresClientAuth() {
			pool, err := poolFromFile(cfg.CAFile)
			if err != nil {
				return nil, err
			}
			tlsConfig.ClientCAs = pool
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
	case config.TLSModeCA:
		if p.pki == nil {
			return nil, errors.New("binding.tls.mode ca needs the daemon CA")
		}
		issued, err := p.pki.IssueServer(p.identity, hostNames()...)
		if err != nil {
			return nil, err
		}
		cert, err := issued.TLSCertificate()
		if err != nil {
			return nil, err
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientCAs:    p.pki.Pool(),
			ClientAuth:   tls.RequireAndVerifyClientCert,
			MinVersion:   tls.VersionTLS13,
		}
	default:
		return nil, fmt.Errorf("unknown binding.tls.mode %q", cfg.Mode)
	}
	for i := range tlsConfig.Certificates {
		p.ring.Remember(&tlsConfig.Certificates[i])
	}
	return tlsConfig, nil
}

// ClientForDaemon dials a daemon-kind environment: its CA pins the server,
// else the system roots verify it, such as the certificate `tailscale serve`
// presents. A client certificate goes along when configured.
func (p *TLSProvider) ClientForDaemon(entry config.EnvironmentEntry) (*tls.Config, error) {
	host, _, err := net.SplitHostPort(entry.Address)
	if err != nil {
		host = entry.Address
	}
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS13}
	if entry.TLS.CAFile != "" {
		if tlsConfig.RootCAs, err = poolFromFile(entry.TLS.CAFile); err != nil {
			return nil, err
		}
	}
	if entry.TLS.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(entry.TLS.CertFile, entry.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading the client certificate for environment %s: %w", entry.Name, err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

// ClientForKubernetes dials a pod daemon: it pins the daemon CA and presents a
// client certificate naming this daemon.
func (p *TLSProvider) ClientForKubernetes(serverName string) (*tls.Config, error) {
	if p.pki == nil {
		return nil, errors.New("a kubernetes environment needs the daemon CA")
	}
	issued, err := p.pki.IssueClient(p.identity)
	if err != nil {
		return nil, err
	}
	cert, err := issued.TLSCertificate()
	if err != nil {
		return nil, err
	}
	return &tls.Config{RootCAs: p.pki.Pool(), Certificates: []tls.Certificate{cert}, ServerName: serverName, MinVersion: tls.VersionTLS13}, nil
}

func poolFromFile(path string) (*x509.CertPool, error) {
	if path == "" {
		return nil, errors.New("a CA file is required to verify TLS peers")
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("the CA file %s holds no certificate", path)
	}
	return pool, nil
}

// hostNames are the names a binding's own certificate covers.
func hostNames() []string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return nil
	}
	return []string{host}
}

// ClientForBinding is how a client on this machine dials this daemon's own
// binding when it serves TLS: the daemon CA signs it a client certificate, and
// certificate files are pinned by their CA.
func (p *TLSProvider) ClientForBinding(cfg config.BindingTLSConfig) (*tls.Config, error) {
	switch cfg.Mode {
	case "", config.TLSModeOff:
		return nil, nil
	case config.TLSModeFiles:
		tlsConfig := &tls.Config{ServerName: "localhost", MinVersion: tls.VersionTLS13}
		if cfg.CAFile != "" {
			pool, err := poolFromFile(cfg.CAFile)
			if err != nil {
				return nil, err
			}
			tlsConfig.RootCAs = pool
		}
		return tlsConfig, nil
	case config.TLSModeCA:
		if p.pki == nil {
			return nil, errors.New("binding.tls.mode ca needs the daemon CA")
		}
		issued, err := p.pki.IssueClient(p.identity)
		if err != nil {
			return nil, err
		}
		cert, err := issued.TLSCertificate()
		if err != nil {
			return nil, err
		}
		return &tls.Config{RootCAs: p.pki.Pool(), Certificates: []tls.Certificate{cert}, ServerName: "localhost", MinVersion: tls.VersionTLS13}, nil
	default:
		return nil, fmt.Errorf("unknown binding.tls.mode %q", cfg.Mode)
	}
}
