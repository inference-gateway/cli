package infrastructure

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	spiffeid "github.com/spiffe/go-spiffe/v2/spiffeid"
	spiffetlsconfig "github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	spiffeworkloadapi "github.com/spiffe/go-spiffe/v2/workloadapi"

	config "github.com/inference-gateway/cli/config"
)

// spiffeSourceTimeout bounds the first SVID fetch from the Workload API, so
// a daemon without a reachable SPIRE agent fails its boot instead of hanging.
const spiffeSourceTimeout = 30 * time.Second

// TLSProvider builds every tls.Config the daemon needs from daemon.yaml: the
// binding's server side and the client side for each executor, over the
// daemon-managed CA or the SPIRE Workload API. One SPIFFE source serves both
// sides and rotates on its own.
type TLSProvider struct {
	identity string
	pki      *PKI
	spiffe   config.SPIFFEConfig
	source   *spiffeworkloadapi.X509Source
	ring     *Keyring
}

// NewTLSProvider wires the provider over the daemon's identity, PKI and SPIFFE
// settings. The Workload API is dialed lazily, on the first config that needs it.
func NewTLSProvider(identity string, pki *PKI, spiffe config.SPIFFEConfig) *TLSProvider {
	return &TLSProvider{identity: identity, pki: pki, spiffe: spiffe, ring: &Keyring{}}
}

// Keyring holds the keys of the certificates the binding served, for opening
// credentials sealed to them.
func (p *TLSProvider) Keyring() *Keyring {
	return p.ring
}

// Close releases the Workload API connection.
func (p *TLSProvider) Close() {
	if p.source != nil {
		_ = p.source.Close()
	}
}

// Server builds the binding's TLS config for its mode, nil for off.
func (p *TLSProvider) Server(ctx context.Context, cfg config.BindingTLSConfig) (*tls.Config, error) {
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
	case config.TLSModeSPIFFE:
		source, err := p.spiffeSource(ctx)
		if err != nil {
			return nil, err
		}
		authorizer, err := p.serverAuthorizer(cfg.SPIFFE)
		if err != nil {
			return nil, err
		}
		tlsConfig = spiffetlsconfig.MTLSServerConfig(source, source, authorizer)
	default:
		return nil, fmt.Errorf("unknown binding.tls.mode %q", cfg.Mode)
	}
	p.rememberServed(tlsConfig)
	return tlsConfig, nil
}

// rememberServed records every certificate the config serves, static ones now
// and rotated ones as they are picked, so sealed credentials open.
func (p *TLSProvider) rememberServed(cfg *tls.Config) {
	for i := range cfg.Certificates {
		p.ring.Remember(&cfg.Certificates[i])
	}
	if pick := cfg.GetCertificate; pick != nil {
		cfg.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, err := pick(hello)
			if err == nil && cert != nil {
				p.ring.Remember(cert)
			}
			return cert, err
		}
	}
}

// ClientForDaemon dials a daemon-kind executor: its CA pins the server, and
// a client certificate goes along when configured.
func (p *TLSProvider) ClientForDaemon(entry config.ExecutorEntry) (*tls.Config, error) {
	pool, err := poolFromFile(entry.TLS.CAFile)
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(entry.Address)
	if err != nil {
		host = entry.Address
	}
	tlsConfig := &tls.Config{RootCAs: pool, ServerName: host, MinVersion: tls.VersionTLS13}
	if entry.TLS.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(entry.TLS.CertFile, entry.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading the client certificate for executor %s: %w", entry.Name, err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

// ClientForKubernetes dials a pod daemon: with the daemon CA it pins the CA
// and presents a client certificate naming this daemon, with SPIFFE it
// presents its SVID and expects the pod's.
func (p *TLSProvider) ClientForKubernetes(ctx context.Context, entry config.ExecutorEntry, serverName string) (*tls.Config, error) {
	switch entry.EffectiveAuth() {
	case config.ExecutorAuthCA:
		if p.pki == nil {
			return nil, errors.New("executor auth ca needs the daemon CA")
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
	case config.ExecutorAuthSPIFFE:
		source, err := p.spiffeSource(ctx)
		if err != nil {
			return nil, err
		}
		authorizer, err := peerAuthorizer(entry.SPIFFE.TrustDomain, entry.SPIFFE.ServerID)
		if err != nil {
			return nil, err
		}
		return spiffetlsconfig.MTLSClientConfig(source, source, authorizer), nil
	default:
		return nil, fmt.Errorf("unknown auth %q for executor %s", entry.Auth, entry.Name)
	}
}

func (p *TLSProvider) spiffeSource(ctx context.Context) (*spiffeworkloadapi.X509Source, error) {
	if p.source != nil {
		return p.source, nil
	}
	socket := p.spiffe.Socket
	if socket == "" {
		socket = config.DefaultSPIFFESocket
	}
	ctx, cancel := context.WithTimeout(ctx, spiffeSourceTimeout)
	defer cancel()
	source, err := spiffeworkloadapi.NewX509Source(ctx, spiffeworkloadapi.WithClientOptions(spiffeworkloadapi.WithAddr(socket)))
	if err != nil {
		return nil, fmt.Errorf("fetching an SVID from the SPIRE Workload API at %s: %w", socket, err)
	}
	p.source = source
	return source, nil
}

func (p *TLSProvider) serverAuthorizer(cfg config.SPIFFEConfig) (spiffetlsconfig.Authorizer, error) {
	if len(cfg.AllowedIDs) == 0 {
		return peerAuthorizer(cfg.TrustDomain, "")
	}
	ids := make([]spiffeid.ID, 0, len(cfg.AllowedIDs))
	for _, raw := range cfg.AllowedIDs {
		id, err := spiffeid.FromString(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed SPIFFE ID %q: %w", raw, err)
		}
		ids = append(ids, id)
	}
	return spiffetlsconfig.AuthorizeOneOf(ids...), nil
}

// peerAuthorizer accepts one SPIFFE ID when named, else any member of the
// trust domain.
func peerAuthorizer(trustDomain, id string) (spiffetlsconfig.Authorizer, error) {
	if id != "" {
		parsed, err := spiffeid.FromString(id)
		if err != nil {
			return nil, fmt.Errorf("invalid SPIFFE ID %q: %w", id, err)
		}
		return spiffetlsconfig.AuthorizeID(parsed), nil
	}
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("invalid SPIFFE trust domain %q: %w", trustDomain, err)
	}
	return spiffetlsconfig.AuthorizeMemberOf(td), nil
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
// binding when it serves TLS: the daemon CA signs it a client certificate, a
// SPIFFE binding takes its SVID, and certificate files are pinned by their CA.
func (p *TLSProvider) ClientForBinding(ctx context.Context, cfg config.BindingTLSConfig) (*tls.Config, error) {
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
	case config.TLSModeSPIFFE:
		source, err := p.spiffeSource(ctx)
		if err != nil {
			return nil, err
		}
		authorizer, err := peerAuthorizer(cfg.SPIFFE.TrustDomain, "")
		if err != nil {
			return nil, err
		}
		return spiffetlsconfig.MTLSClientConfig(source, source, authorizer), nil
	default:
		return nil, fmt.Errorf("unknown binding.tls.mode %q", cfg.Mode)
	}
}
