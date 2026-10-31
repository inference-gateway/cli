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
			if got.Dialable() != (tt.want.Enabled && (tt.want.Token != "" || tt.want.TLS.RequiresClientAuth())) {
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
	kubernetes := WorkspaceEntry{Name: "k8s", Kind: WorkspaceKindKubernetes, Namespace: "infer", Image: "infer:local"}
	daemon := WorkspaceEntry{Name: "ws", Kind: WorkspaceKindDaemon, Address: "ws.lan:52789", TLS: PeerTLSConfig{CAFile: "ca.crt"}}
	tests := []struct {
		name    string
		cfg     DaemonConfig
		wantErr string
	}{
		{"defaults", *DefaultDaemonConfig(), ""},
		{"valid workspaces", DaemonConfig{Workspace: "laptop", Workspaces: []WorkspaceEntry{{Name: "laptop", Kind: WorkspaceKindLocal}, kubernetes, daemon}}, ""},
		{"unknown tls mode", DaemonConfig{Binding: BindingConfig{TLS: BindingTLSConfig{Mode: "pem"}}}, "binding.tls.mode"},
		{"files mode needs files", DaemonConfig{Binding: BindingConfig{TLS: BindingTLSConfig{Mode: TLSModeFiles}}}, "cert_file and key_file"},
		{"remote host needs tls", DaemonConfig{Binding: BindingConfig{Host: "0.0.0.0"}}, "must not be off"},
		{"remote host with tls", DaemonConfig{Binding: BindingConfig{Host: "0.0.0.0", TLS: BindingTLSConfig{Mode: TLSModeCA}}}, ""},
		{"duplicate names", DaemonConfig{Workspaces: []WorkspaceEntry{kubernetes, kubernetes}}, "twice"},
		{"nameless", DaemonConfig{Workspaces: []WorkspaceEntry{{Kind: WorkspaceKindDaemon}}}, "needs a name"},
		{"unknown kind", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "x", Kind: "vm"}}}, "invalid kind"},
		{"two locals", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "a", Kind: WorkspaceKindLocal}, {Name: "b", Kind: WorkspaceKindLocal}}}, "only one workspace"},
		{"local named after the identity", DaemonConfig{Workspace: "laptop", Workspaces: []WorkspaceEntry{{Name: "other", Kind: WorkspaceKindLocal}}}, "named after daemon.workspace"},
		{"daemon kind needs an address", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "ws", Kind: WorkspaceKindDaemon, TLS: PeerTLSConfig{CAFile: "ca"}}}}, "needs an address"},
		{"daemon kind trusts the system roots without a ca_file", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "ws", Kind: WorkspaceKindDaemon, Address: "a:1"}}}, ""},
		{"kubernetes needs namespace and image", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "k8s", Kind: WorkspaceKindKubernetes}}}, "namespace and an image"},
		{"kubernetes runs the gateway all-in-one", DaemonConfig{Workspaces: []WorkspaceEntry{withMode(kubernetes, WorkspaceModeAllInOne)}}, ""},
		{"unknown mode", DaemonConfig{Workspaces: []WorkspaceEntry{withMode(kubernetes, "sidecar")}}, "invalid mode"},
		{"mode on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{withMode(daemon, WorkspaceModeStandard)}}, "only a kubernetes workspace"},
		{"gateway_env on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "ws", Kind: WorkspaceKindDaemon, Address: "a:1", GatewayEnv: []string{"OPENAI_API_KEY"}}}}, "only a kubernetes workspace"},
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
	if got := (DaemonConfig{Workspace: "laptop"}).Identity(); got != "laptop" {
		t.Fatalf("Identity() = %q, want laptop", got)
	}
	if got := (DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "desk", Kind: WorkspaceKindLocal}}}).Identity(); got != "desk" {
		t.Fatalf("Identity() = %q, want the local entry's name", got)
	}
	if got := (DaemonConfig{}).Identity(); got == "" {
		t.Fatal("Identity() falls back to the hostname, never empty")
	}

	k8s := WorkspaceEntry{Kind: WorkspaceKindKubernetes}
	if got := k8s.EffectiveLeaseTTL(); got != 2*time.Hour+5*time.Minute {
		t.Fatalf("kubernetes lease ttl = %v", got)
	}
	k8s.IdleTTL = time.Hour
	if got := k8s.EffectiveLeaseTTL(); got != time.Hour+5*time.Minute {
		t.Fatalf("kubernetes lease ttl follows idle ttl: %v", got)
	}
	if got := k8s.EffectiveAwayTTL(); got != time.Minute {
		t.Fatalf("kubernetes away ttl = %v, want the minute default", got)
	}
	k8s.AwayTTL = 10 * time.Minute
	if got := k8s.EffectiveAwayTTL(); got != 10*time.Minute {
		t.Fatalf("configured away ttl wins: %v", got)
	}
	if got := DefaultDaemonConfig().AwayExit; got != time.Minute {
		t.Fatalf("default away_exit = %v, want a minute", got)
	}
	if got := (WorkspaceEntry{Kind: WorkspaceKindDaemon}).EffectiveLeaseTTL(); got != 24*time.Hour {
		t.Fatalf("daemon lease ttl = %v", got)
	}
	if got := (WorkspaceEntry{Kind: WorkspaceKindDaemon, LeaseTTL: time.Minute}).EffectiveLeaseTTL(); got != time.Minute {
		t.Fatalf("configured lease ttl wins: %v", got)
	}
}

func TestBindingDialableWithATokenOrAClientCertificate(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
		want    bool
	}{
		{"off", Binding{Token: "secret"}, false},
		{"on without a token or certificates", Binding{Enabled: true}, false},
		{"on with a token", Binding{Enabled: true, Token: "secret"}, true},
		{"on with the ca mode and no token", Binding{Enabled: true, TLS: BindingTLSConfig{Mode: TLSModeCA}}, true},
		{"on with files and client auth", Binding{Enabled: true, TLS: BindingTLSConfig{Mode: TLSModeFiles, ClientAuth: true}}, true},
		{"on with files and no client auth", Binding{Enabled: true, TLS: BindingTLSConfig{Mode: TLSModeFiles}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.binding.Dialable(); got != tt.want {
				t.Fatalf("Dialable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func withMode(entry WorkspaceEntry, mode WorkspaceMode) WorkspaceEntry {
	entry.Mode = mode
	return entry
}
