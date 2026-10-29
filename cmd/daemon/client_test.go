package daemon

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestAuthenticatesWithATokenOrAClientCertificate(t *testing.T) {
	tests := []struct {
		name    string
		binding config.Binding
		want    bool
	}{
		{"off", config.Binding{Token: "secret"}, false},
		{"on without a token or certificates", config.Binding{Enabled: true}, false},
		{"on with a token", config.Binding{Enabled: true, Token: "secret"}, true},
		{"on with the ca mode and no token", config.Binding{Enabled: true, TLS: config.BindingTLSConfig{Mode: config.TLSModeCA}}, true},
		{"on with files and client auth", config.Binding{Enabled: true, TLS: config.BindingTLSConfig{Mode: config.TLSModeFiles, ClientAuth: true}}, true},
		{"on with files and no client auth", config.Binding{Enabled: true, TLS: config.BindingTLSConfig{Mode: config.TLSModeFiles}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := authenticates(tt.binding); got != tt.want {
				t.Fatalf("authenticates(%+v) = %v, want %v", tt.binding, got, tt.want)
			}
		})
	}
}
