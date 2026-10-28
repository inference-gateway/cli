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
	kubernetes := ExecutorEntry{Name: "k8s", Kind: ExecutorKindKubernetes, Namespace: "infer", Image: "infer:local"}
	daemon := ExecutorEntry{Name: "ws", Kind: ExecutorKindDaemon, Address: "ws.lan:52789", TLS: PeerTLSConfig{CAFile: "ca.crt"}}
	tests := []struct {
		name    string
		cfg     DaemonConfig
		wantErr string
	}{
		{"defaults", *DefaultDaemonConfig(), ""},
		{"valid executors", DaemonConfig{Executor: "laptop", Executors: []ExecutorEntry{{Name: "laptop", Kind: ExecutorKindLocal}, kubernetes, daemon}}, ""},
		{"unknown tls mode", DaemonConfig{Binding: BindingConfig{TLS: BindingTLSConfig{Mode: "pem"}}}, "binding.tls.mode"},
		{"files mode needs files", DaemonConfig{Binding: BindingConfig{TLS: BindingTLSConfig{Mode: TLSModeFiles}}}, "cert_file and key_file"},
		{"spiffe mode needs a trust domain", DaemonConfig{Binding: BindingConfig{TLS: BindingTLSConfig{Mode: TLSModeSPIFFE}}}, "trust_domain"},
		{"remote host needs tls", DaemonConfig{Binding: BindingConfig{Host: "0.0.0.0"}}, "must not be off"},
		{"remote host with tls", DaemonConfig{Binding: BindingConfig{Host: "0.0.0.0", TLS: BindingTLSConfig{Mode: TLSModeCA}}}, ""},
		{"duplicate names", DaemonConfig{Executors: []ExecutorEntry{kubernetes, kubernetes}}, "twice"},
		{"nameless", DaemonConfig{Executors: []ExecutorEntry{{Kind: ExecutorKindDaemon}}}, "needs a name"},
		{"unknown kind", DaemonConfig{Executors: []ExecutorEntry{{Name: "x", Kind: "vm"}}}, "invalid kind"},
		{"two locals", DaemonConfig{Executors: []ExecutorEntry{{Name: "a", Kind: ExecutorKindLocal}, {Name: "b", Kind: ExecutorKindLocal}}}, "only one executor"},
		{"local named after the identity", DaemonConfig{Executor: "laptop", Executors: []ExecutorEntry{{Name: "other", Kind: ExecutorKindLocal}}}, "named after daemon.executor"},
		{"daemon kind needs an address", DaemonConfig{Executors: []ExecutorEntry{{Name: "ws", Kind: ExecutorKindDaemon, TLS: PeerTLSConfig{CAFile: "ca"}}}}, "needs an address"},
		{"daemon kind needs tls", DaemonConfig{Executors: []ExecutorEntry{{Name: "ws", Kind: ExecutorKindDaemon, Address: "a:1"}}}, "require TLS"},
		{"kubernetes needs namespace and image", DaemonConfig{Executors: []ExecutorEntry{{Name: "k8s", Kind: ExecutorKindKubernetes}}}, "namespace and an image"},
		{"kubernetes spiffe needs a trust domain", DaemonConfig{Executors: []ExecutorEntry{{Name: "k8s", Kind: ExecutorKindKubernetes, Namespace: "n", Image: "i", Auth: ExecutorAuthSPIFFE}}}, "spiffe.trust_domain"},
		{"kubernetes unknown auth", DaemonConfig{Executors: []ExecutorEntry{{Name: "k8s", Kind: ExecutorKindKubernetes, Namespace: "n", Image: "i", Auth: "token"}}}, "invalid auth"},
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
	if got := (DaemonConfig{Executor: "laptop"}).Identity(); got != "laptop" {
		t.Fatalf("Identity() = %q, want laptop", got)
	}
	if got := (DaemonConfig{Executors: []ExecutorEntry{{Name: "desk", Kind: ExecutorKindLocal}}}).Identity(); got != "desk" {
		t.Fatalf("Identity() = %q, want the local entry's name", got)
	}
	if got := (DaemonConfig{}).Identity(); got == "" {
		t.Fatal("Identity() falls back to the hostname, never empty")
	}

	k8s := ExecutorEntry{Kind: ExecutorKindKubernetes}
	if got := k8s.EffectiveLeaseTTL(); got != 2*time.Hour+5*time.Minute {
		t.Fatalf("kubernetes lease ttl = %v", got)
	}
	k8s.IdleTTL = time.Hour
	if got := k8s.EffectiveLeaseTTL(); got != time.Hour+5*time.Minute {
		t.Fatalf("kubernetes lease ttl follows idle ttl: %v", got)
	}
	if got := (ExecutorEntry{Kind: ExecutorKindDaemon}).EffectiveLeaseTTL(); got != 24*time.Hour {
		t.Fatalf("daemon lease ttl = %v", got)
	}
	if got := (ExecutorEntry{Kind: ExecutorKindDaemon, LeaseTTL: time.Minute}).EffectiveLeaseTTL(); got != time.Minute {
		t.Fatalf("configured lease ttl wins: %v", got)
	}
	if got := k8s.EffectiveAuth(); got != ExecutorAuthCA {
		t.Fatalf("default auth = %q", got)
	}
}
