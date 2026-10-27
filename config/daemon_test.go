package config

import "testing"

func TestBindingAddressAndConfigured(t *testing.T) {
	tests := []struct {
		name       string
		set        func(c *Config)
		wantPort   int
		wantToken  string
		configured bool
	}{
		{
			name:     "defaults fall back to the extension port and no token",
			set:      func(*Config) {},
			wantPort: DefaultExtensionPort,
		},
		{
			name: "daemon.yaml wins over the extension",
			set: func(c *Config) {
				c.Daemon.Binding = BindingConfig{Enabled: true, Port: 9999, Token: "daemon"}
				c.BrowserUse.Extension.Port = 8888
				c.BrowserUse.Extension.Token = "ext"
			},
			wantPort:   9999,
			wantToken:  "daemon",
			configured: true,
		},
		{
			name: "the extension backend configures the binding",
			set: func(c *Config) {
				c.BrowserUse.Enabled = true
				c.BrowserUse.Backend = BrowserBackendExtension
				c.BrowserUse.Extension.Token = "ext"
			},
			wantPort:   DefaultExtensionPort,
			wantToken:  "ext",
			configured: true,
		},
		{
			name: "enabled without a token is not configured",
			set: func(c *Config) {
				c.Daemon.Binding.Enabled = true
			},
			wantPort: DefaultExtensionPort,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.set(cfg)
			port, token := cfg.BindingAddress()
			if port != tt.wantPort || token != tt.wantToken {
				t.Fatalf("BindingAddress = (%d, %q), want (%d, %q)", port, token, tt.wantPort, tt.wantToken)
			}
			if got := cfg.BindingConfigured(); got != tt.configured {
				t.Fatalf("BindingConfigured = %v, want %v", got, tt.configured)
			}
		})
	}
}
