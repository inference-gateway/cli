package daemon

import (
	"context"
	"errors"
	"path/filepath"

	config "github.com/inference-gateway/cli/config"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// ErrNoBinding is the refusal to dial a daemon whose binding is off.
var ErrNoBinding = errors.New("the daemon binding is off: enable daemon.binding in daemon.yaml")

// LocalDialConfig is how a client of kind dials the daemon cfg describes: the
// binding's address and token, or this machine's local token the daemon also
// uses when daemon.yaml sets none, over TLS with this machine's client
// certificate when the binding serves one.
func LocalDialConfig(ctx context.Context, cfg *config.Config, kind string) (agui.DialConfig, error) {
	if err := FillBindingToken(cfg); err != nil {
		return agui.DialConfig{}, err
	}
	binding := cfg.Binding()
	if !binding.Dialable() {
		return agui.DialConfig{}, ErrNoBinding
	}
	var pki *agentinfra.PKI
	if binding.TLS.Mode == config.TLSModeCA {
		var err error
		if pki, err = agentinfra.LoadOrCreatePKI(filepath.Join(config.UserSpaceConfigDir(), "daemon", "pki")); err != nil {
			return agui.DialConfig{}, err
		}
	}
	tlsConfig, err := agentinfra.NewTLSProvider(kind, pki).ClientForBinding(binding.TLS)
	if err != nil {
		return agui.DialConfig{}, err
	}
	return agui.DialConfig{
		Addr: binding.Addr(), Token: binding.Token, Kind: kind, Handshake: browserinfra.BindingHandshake, TLS: tlsConfig,
	}, nil
}
