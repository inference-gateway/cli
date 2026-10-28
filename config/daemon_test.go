package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

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
				c.Daemon.Binding.Enabled = true
				c.Daemon.Binding.Port = 9999
				c.Daemon.Binding.Token = "daemon"
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
			tt.want.TLS = DefaultDaemonConfig().Binding.TLS
			got := cfg.Binding()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Binding() = %+v, want %+v", got, tt.want)
			}
			if got.Dialable() != (tt.want.Enabled && tt.want.Token != "") {
				t.Fatalf("Dialable() = %v for %+v", got.Dialable(), got)
			}
		})
	}
}

func TestBindingAddr(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"", "127.0.0.1:52789"},
		{"0.0.0.0", "127.0.0.1:52789"},
		{"::", "127.0.0.1:52789"},
		{"127.0.0.1", "127.0.0.1:52789"},
		{"10.0.0.7", "10.0.0.7:52789"},
	}
	for _, tt := range tests {
		if got := (Binding{Host: tt.host, Port: 52789}).Addr(); got != tt.want {
			t.Fatalf("Addr() with host %q = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestDaemonConfigValidate(t *testing.T) {
	kubernetes := EnvironmentEntry{Name: "k8s", Kind: EnvironmentKindKubernetes, Namespace: "infer", Image: "infer:local"}
	daemon := EnvironmentEntry{Name: "ws", Kind: EnvironmentKindDaemon, Address: "ws.lan:52789", TLS: PeerTLSConfig{CAFile: "ca.crt"}}
	tests := []struct {
		name    string
		cfg     DaemonConfig
		wantErr string
	}{
		{"defaults", *DefaultDaemonConfig(), ""},
		{"valid environments", DaemonConfig{Environment: "laptop", Environments: []EnvironmentEntry{{Name: "laptop", Kind: EnvironmentKindLocal}, kubernetes, daemon}}, ""},
		{"unknown tls mode", DaemonConfig{Binding: BindingConfig{TLS: BindingTLSConfig{Mode: "pem"}}}, "binding.tls.mode"},
		{"files mode needs files", DaemonConfig{Binding: BindingConfig{TLS: BindingTLSConfig{Mode: TLSModeFiles}}}, "cert_file and key_file"},
		{"spiffe mode needs a trust domain", DaemonConfig{Binding: BindingConfig{TLS: BindingTLSConfig{Mode: TLSModeSPIFFE}}}, "trust_domain"},
		{"remote host needs tls", DaemonConfig{Binding: BindingConfig{Host: "0.0.0.0"}}, "must not be off"},
		{"remote host with tls", DaemonConfig{Binding: BindingConfig{Host: "0.0.0.0", TLS: BindingTLSConfig{Mode: TLSModeCA}}}, ""},
		{"duplicate names", DaemonConfig{Environments: []EnvironmentEntry{kubernetes, kubernetes}}, "twice"},
		{"nameless", DaemonConfig{Environments: []EnvironmentEntry{{Kind: EnvironmentKindDaemon}}}, "needs a name"},
		{"unknown kind", DaemonConfig{Environments: []EnvironmentEntry{{Name: "x", Kind: "vm"}}}, "invalid kind"},
		{"two locals", DaemonConfig{Environments: []EnvironmentEntry{{Name: "a", Kind: EnvironmentKindLocal}, {Name: "b", Kind: EnvironmentKindLocal}}}, "only one environment"},
		{"local named after the identity", DaemonConfig{Environment: "laptop", Environments: []EnvironmentEntry{{Name: "other", Kind: EnvironmentKindLocal}}}, "named after daemon.environment"},
		{"daemon kind needs an address", DaemonConfig{Environments: []EnvironmentEntry{{Name: "ws", Kind: EnvironmentKindDaemon, TLS: PeerTLSConfig{CAFile: "ca"}}}}, "needs an address"},
		{"daemon kind needs tls", DaemonConfig{Environments: []EnvironmentEntry{{Name: "ws", Kind: EnvironmentKindDaemon, Address: "a:1"}}}, "require TLS"},
		{"kubernetes needs namespace and image", DaemonConfig{Environments: []EnvironmentEntry{{Name: "k8s", Kind: EnvironmentKindKubernetes}}}, "namespace and an image"},
		{"kubernetes spiffe needs a trust domain", DaemonConfig{Environments: []EnvironmentEntry{{Name: "k8s", Kind: EnvironmentKindKubernetes, Namespace: "n", Image: "i", Auth: EnvironmentAuthSPIFFE}}}, "spiffe.trust_domain"},
		{"kubernetes unknown auth", DaemonConfig{Environments: []EnvironmentEntry{{Name: "k8s", Kind: EnvironmentKindKubernetes, Namespace: "n", Image: "i", Auth: "token"}}}, "invalid auth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDaemonConfigIdentityAndTTLs(t *testing.T) {
	if got := (DaemonConfig{Environment: "laptop"}).Identity(); got != "laptop" {
		t.Fatalf("Identity() = %q, want laptop", got)
	}
	if got := (DaemonConfig{Environments: []EnvironmentEntry{{Name: "desk", Kind: EnvironmentKindLocal}}}).Identity(); got != "desk" {
		t.Fatalf("Identity() = %q, want the local entry's name", got)
	}
	if got := (DaemonConfig{}).Identity(); got == "" {
		t.Fatal("Identity() falls back to the hostname, never empty")
	}

	k8s := EnvironmentEntry{Kind: EnvironmentKindKubernetes}
	if got := k8s.EffectiveLeaseTTL(); got != 2*time.Hour+5*time.Minute {
		t.Fatalf("kubernetes lease ttl = %v", got)
	}
	k8s.IdleTTL = time.Hour
	if got := k8s.EffectiveLeaseTTL(); got != time.Hour+5*time.Minute {
		t.Fatalf("kubernetes lease ttl follows idle ttl: %v", got)
	}
	if got := (EnvironmentEntry{Kind: EnvironmentKindDaemon}).EffectiveLeaseTTL(); got != 24*time.Hour {
		t.Fatalf("daemon lease ttl = %v", got)
	}
	if got := (EnvironmentEntry{Kind: EnvironmentKindDaemon, LeaseTTL: time.Minute}).EffectiveLeaseTTL(); got != time.Minute {
		t.Fatalf("configured lease ttl wins: %v", got)
	}
	if got := k8s.EffectiveAuth(); got != EnvironmentAuthCA {
		t.Fatalf("default auth = %q", got)
	}
}
