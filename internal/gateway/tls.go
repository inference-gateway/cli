package gateway

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"

	config "github.com/inference-gateway/cli/config"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// Transport is how infer reaches the gateway: gateway.tls.ca_file is trusted on
// top of the system roots and the gateway.tls client certificate is presented.
// Unusable TLS material fails every request with its cause, rather than falling
// back to a transport the gateway would reject.
func Transport(cfg config.PeerTLSConfig) http.RoundTripper {
	if cfg == (config.PeerTLSConfig{}) {
		return http.DefaultTransport
	}
	tlsConfig, err := clientTLSConfig(cfg)
	if err != nil {
		return failingTransport{err: err}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return transport
}

func clientTLSConfig(cfg config.PeerTLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pool, err := systemRootsWith(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.CertFile == "" && cfg.KeyFile == "" {
		return tlsConfig, nil
	}
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, errors.New("gateway.tls.cert_file and gateway.tls.key_file must be set together")
	}
	cert, err := tls.LoadX509KeyPair(utils.ExpandHome(cfg.CertFile), utils.ExpandHome(cfg.KeyFile))
	if err != nil {
		return nil, fmt.Errorf("loading gateway.tls.cert_file and gateway.tls.key_file: %w", err)
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	return tlsConfig, nil
}

// systemRootsWith adds the CA in caFile to the system roots, so a private
// gateway CA does not cost the trust every other HTTPS call relies on.
func systemRootsWith(caFile string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(utils.ExpandHome(caFile))
	if err != nil {
		return nil, fmt.Errorf("reading gateway.tls.ca_file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("gateway.tls.ca_file %s holds no certificate", caFile)
	}
	return pool, nil
}

type failingTransport struct {
	err error
}

func (t failingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
	return nil, t.err
}
