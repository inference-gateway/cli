package config

import "testing"

func TestBindingResolution(t *testing.T) {
	tests := []struct {
		name string
		set  func(c *Config)
		want Binding
	}{
		{
			name: "defaults fall back to the extension port and no token",
			set:  func(*Config) {},
			want: Binding{Port: DefaultExtensionPort},
		},
		{
			name: "daemon.yaml wins over the extension",
			set: func(c *Config) {
				c.Daemon.Binding = BindingConfig{Enabled: true, Port: 9999, Token: "daemon"}
				c.BrowserUse.Extension.Port = 8888
				c.BrowserUse.Extension.Token = "ext"
			},
			want: Binding{Enabled: true, Port: 9999, Token: "daemon"},
		},
		{
			name: "the extension backend turns the binding on",
			set: func(c *Config) {
				c.BrowserUse.Enabled = true
				c.BrowserUse.Backend = BrowserBackendExtension
				c.BrowserUse.Extension.Token = "ext"
			},
			want: Binding{Enabled: true, Port: DefaultExtensionPort, Token: "ext"},
		},
		{
			name: "enabled without a token is not dialable",
			set: func(c *Config) {
				c.Daemon.Binding.Enabled = true
			},
			want: Binding{Enabled: true, Port: DefaultExtensionPort},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.set(cfg)
			got := cfg.Binding()
			if got != tt.want {
				t.Fatalf("Binding() = %+v, want %+v", got, tt.want)
			}
			if got.Dialable() != (tt.want.Enabled && tt.want.Token != "") {
				t.Fatalf("Dialable() = %v for %+v", got.Dialable(), got)
			}
		})
	}
}

func TestBindingAddr(t *testing.T) {
	if got := (Binding{Port: 52789}).Addr(); got != "127.0.0.1:52789" {
		t.Fatalf("Addr() = %q", got)
	}
}
