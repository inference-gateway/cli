package daemon

import (
	"errors"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestLocalDialConfigDialsWithTheBindingsCredentials(t *testing.T) {
	tests := []struct {
		name     string
		binding  config.BindingConfig
		wantErr  error
		wantTLS  bool
		wantCert bool
	}{
		{name: "binding off", binding: config.BindingConfig{Token: "secret"}, wantErr: ErrNoBinding},
		{name: "no token and no client certificates", binding: config.BindingConfig{Enabled: true}, wantErr: ErrNoBinding},
		{name: "token over plain loopback", binding: config.BindingConfig{Enabled: true, Token: "secret"}},
		{name: "client certificate from the daemon CA, no token", binding: config.BindingConfig{Enabled: true, TLS: config.BindingTLSConfig{Mode: config.TLSModeCA}}, wantTLS: true, wantCert: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			cfg := config.DefaultConfig()
			cfg.Daemon.Binding = tt.binding
			dial, err := LocalDialConfig(t.Context(), cfg, "tui")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if dial.Kind != "tui" || dial.Token != tt.binding.Token || dial.Addr == "" || dial.Handshake.Hello == "" {
				t.Errorf("dial = %+v, want kind tui with the binding's address, token and handshake", dial)
			}
			if (dial.TLS != nil) != tt.wantTLS || (tt.wantCert && len(dial.TLS.Certificates) != 1) {
				t.Errorf("tls = %+v, want tls %v with a client certificate %v", dial.TLS, tt.wantTLS, tt.wantCert)
			}
		})
	}
}
