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
		{"protocol a2a", DaemonConfig{Workspaces: []WorkspaceEntry{withProtocol(kubernetes, WorkspaceProtocolA2A)}}, ""},
		{"protocol agui", DaemonConfig{Workspaces: []WorkspaceEntry{withProtocol(kubernetes, WorkspaceProtocolAGUI)}}, ""},
		{"unknown protocol", DaemonConfig{Workspaces: []WorkspaceEntry{withProtocol(kubernetes, "grpc")}}, "invalid protocol"},
		{"mode on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{withMode(daemon, WorkspaceModeStandard)}}, "only a kubernetes workspace"},
		{"gateway_env on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "ws", Kind: WorkspaceKindDaemon, Address: "a:1", GatewayEnv: []string{"OPENAI_API_KEY"}}}}, "only a kubernetes workspace"},
		{"secrets on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "ws", Kind: WorkspaceKindDaemon, Address: "a:1", Secrets: []string{"agent-credentials"}}}}, "only a kubernetes workspace"},
		{"mcp_servers on a local kind", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "ws", Kind: WorkspaceKindLocal, MCPServers: []string{"mailbox"}}}}, "only a kubernetes workspace"},
		{"binding_service on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "ws", Kind: WorkspaceKindDaemon, Address: "a:1", BindingService: BindingServiceConfig{Labels: map[string]string{"a": "b"}}}}}, "only a kubernetes workspace"},
		{"user_data on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "ws", Kind: WorkspaceKindDaemon, Address: "a:1", UserData: "#!/bin/sh\n"}}}, "only a kubernetes workspace"},
		{"user_data script", DaemonConfig{Workspaces: []WorkspaceEntry{withUserData(kubernetes, "#!/bin/sh\napt-get install -y firefox-esr\n")}}, ""},
		{"user_data cloud-config", DaemonConfig{Workspaces: []WorkspaceEntry{withUserData(kubernetes, "#cloud-config\npackages:\n  - firefox-esr\n")}}, "must start with \"#!\""},
		{"volumes on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{withVolumes(daemon, WorkspaceVolume{Name: "footage", Claim: "raw-footage", MountPath: "/mnt/footage"})}}, "only a kubernetes workspace"},
		{"volumes, one of them read-only", DaemonConfig{Workspaces: []WorkspaceEntry{withVolumes(kubernetes,
			WorkspaceVolume{Name: "resolve", Claim: "davinci-resolve", MountPath: "/opt/resolve"},
			WorkspaceVolume{Name: "footage", Claim: "raw-footage", MountPath: "/mnt/footage", ReadOnly: true})}}, ""},
		{"volume without a name", DaemonConfig{Workspaces: []WorkspaceEntry{withVolumes(kubernetes, WorkspaceVolume{Claim: "raw-footage", MountPath: "/mnt/footage"})}}, "volumes[0] needs a name"},
		{"volume without a claim", DaemonConfig{Workspaces: []WorkspaceEntry{withVolumes(kubernetes, WorkspaceVolume{Name: "footage", MountPath: "/mnt/footage"})}}, "volume \"footage\" needs a claim"},
		{"volume with a relative mount_path", DaemonConfig{Workspaces: []WorkspaceEntry{withVolumes(kubernetes, WorkspaceVolume{Name: "footage", Claim: "raw-footage", MountPath: "mnt/footage"})}}, "needs an absolute mount_path"},
		{"volume without a mount_path", DaemonConfig{Workspaces: []WorkspaceEntry{withVolumes(kubernetes, WorkspaceVolume{Name: "footage", Claim: "raw-footage"})}}, "needs an absolute mount_path"},
		{"two volumes of one name", DaemonConfig{Workspaces: []WorkspaceEntry{withVolumes(kubernetes,
			WorkspaceVolume{Name: "footage", Claim: "a", MountPath: "/mnt/a"},
			WorkspaceVolume{Name: "footage", Claim: "b", MountPath: "/mnt/b"})}}, "two volumes named \"footage\""},
		{"resources on a daemon kind", DaemonConfig{Workspaces: []WorkspaceEntry{withResources(daemon, WorkspaceResources{Limits: map[string]string{"cpu": "2"}})}}, "only a kubernetes workspace"},
		{"resources with requests and limits", DaemonConfig{Workspaces: []WorkspaceEntry{withResources(kubernetes, WorkspaceResources{
			Requests: map[string]string{"cpu": "2", "memory": "4Gi"},
			Limits:   map[string]string{"cpu": "8", "memory": "12Gi", "devic.es/dri": "1"}})}}, ""},
		{"resource limit without a quantity", DaemonConfig{Workspaces: []WorkspaceEntry{withResources(kubernetes, WorkspaceResources{Limits: map[string]string{"memory": " "}})}}, "resources.limits.memory needs a quantity"},
		{"resource request without a name", DaemonConfig{Workspaces: []WorkspaceEntry{withResources(kubernetes, WorkspaceResources{Requests: map[string]string{"": "2"}})}}, "resources.requests has an entry without a resource name"},
		{"kubernetes address template", DaemonConfig{Workspaces: []WorkspaceEntry{withAddress(kubernetes, "{{.Orchestrator}}.ts.net:52789")}}, ""},
		{"kubernetes address template typo", DaemonConfig{Workspaces: []WorkspaceEntry{withAddress(kubernetes, "{{.Pod}}.ts.net:52789")}}, "address of workspace"},
		{"kubernetes annotation template typo", DaemonConfig{Workspaces: []WorkspaceEntry{withAnnotation(kubernetes, "vpn/hostname", "{{.Orchestrator")}}, "binding_service.annotations"},
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

func TestDaemonConfigMaxArtifactBytes(t *testing.T) {
	tests := []struct {
		name string
		mb   int
		want int64
	}{
		{"unset takes the default", 0, 1 << 30},
		{"negative takes the default", -1, 1 << 30},
		{"configured", 64, 64 << 20},
		{"above the default", 4096, 4 << 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (DaemonConfig{MaxArtifactMB: tt.mb}).MaxArtifactBytes(); got != tt.want {
				t.Fatalf("MaxArtifactBytes() = %d, want %d", got, tt.want)
			}
		})
	}
	if got := DefaultDaemonConfig().MaxArtifactMB; got != 1024 {
		t.Fatalf("default max_artifact_mb = %d, want 1024", got)
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

func TestExtensionLoopbackServesTheExtensionOnlyBehindMutualTLS(t *testing.T) {
	mutualTLS := BindingTLSConfig{Mode: TLSModeFiles, CertFile: "c", KeyFile: "k", CAFile: "ca", ClientAuth: true}
	extensionBehind := func(tls BindingTLSConfig) func(c *Config) {
		return func(c *Config) {
			c.BrowserUse.Enabled = true
			c.BrowserUse.Backend = BrowserBackendExtension
			c.BrowserUse.Extension = ExtensionConfig{Port: 52790, Token: "ext"}
			c.Daemon.Binding = BindingConfig{Port: 52789, Token: "daemon", TLS: tls}
		}
	}
	tests := []struct {
		name    string
		set     func(c *Config)
		want    Binding
		wantErr string
	}{
		{
			name: "plain binding keeps one listener",
			set:  extensionBehind(BindingTLSConfig{Mode: TLSModeOff}),
		},
		{
			name: "TLS without client auth keeps one listener",
			set:  extensionBehind(BindingTLSConfig{Mode: TLSModeFiles, CertFile: "c", KeyFile: "k"}),
		},
		{
			name: "client auth without TLS keeps one listener",
			set:  extensionBehind(BindingTLSConfig{Mode: TLSModeOff, ClientAuth: true}),
		},
		{
			name: "mutual TLS without the extension backend keeps one listener",
			set: func(c *Config) {
				extensionBehind(mutualTLS)(c)
				c.BrowserUse.Backend = "playwright"
			},
		},
		{
			name: "mutual TLS serves the extension port and token on loopback",
			set:  extensionBehind(mutualTLS),
			want: Binding{Enabled: true, Port: 52790, Token: "ext"},
		},
		{
			name: "the ca mode serves the extension on loopback",
			set:  extensionBehind(BindingTLSConfig{Mode: TLSModeCA}),
			want: Binding{Enabled: true, Port: 52790, Token: "ext"},
		},
		{
			name: "an unset extension port falls back to the default",
			set: func(c *Config) {
				extensionBehind(mutualTLS)(c)
				c.BrowserUse.Extension.Port = 0
				c.Daemon.Binding.Port = 52800
			},
			want: Binding{Enabled: true, Port: DefaultExtensionPort, Token: "ext"},
		},
		{
			name: "the extension port clashing with the binding port fails",
			set: func(c *Config) {
				extensionBehind(mutualTLS)(c)
				c.Daemon.Binding.Port = 0
			},
			wantErr: "set daemon.binding.port",
		},
		{
			name: "an empty extension token fails",
			set: func(c *Config) {
				extensionBehind(mutualTLS)(c)
				c.BrowserUse.Extension.Token = ""
			},
			wantErr: "browser_use.extension.token is empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.set(cfg)
			got, err := cfg.ExtensionLoopback()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ExtensionLoopback() error = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtensionLoopback() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExtensionLoopback() = %+v, want %+v", got, tt.want)
			}
			if got.Enabled && got.ListenHost() != "127.0.0.1" {
				t.Fatalf("ExtensionLoopback() listens on %q, want loopback", got.ListenHost())
			}
		})
	}
}

func withMode(entry WorkspaceEntry, mode WorkspaceMode) WorkspaceEntry {
	entry.Mode = mode
	return entry
}

func withResources(entry WorkspaceEntry, resources WorkspaceResources) WorkspaceEntry {
	entry.Resources = resources
	return entry
}

func withVolumes(entry WorkspaceEntry, volumes ...WorkspaceVolume) WorkspaceEntry {
	entry.Volumes = volumes
	return entry
}

func withUserData(entry WorkspaceEntry, userData string) WorkspaceEntry {
	entry.UserData = userData
	return entry
}

func withProtocol(entry WorkspaceEntry, protocol WorkspaceProtocol) WorkspaceEntry {
	entry.Protocol = protocol
	return entry
}

func withAddress(entry WorkspaceEntry, address string) WorkspaceEntry {
	entry.Address = address
	return entry
}

func withAnnotation(entry WorkspaceEntry, key, value string) WorkspaceEntry {
	entry.BindingService.Annotations = map[string]string{key: value}
	return entry
}

func TestWorkspaceEntryForOrchestrator(t *testing.T) {
	entry := WorkspaceEntry{Name: "k8s", Address: "{{.Orchestrator}}.ts.net:52789", BindingService: BindingServiceConfig{
		Annotations: map[string]string{"vpn/hostname": "{{.Orchestrator}}", "vpn/expose": "true"},
		Labels:      map[string]string{"session": "{{.Orchestrator}}"},
	}}
	expanded, err := entry.ForOrchestrator("infer-abc")
	if err != nil {
		t.Fatal(err)
	}
	if expanded.Address != "infer-abc.ts.net:52789" {
		t.Fatalf("Address = %q", expanded.Address)
	}
	if expanded.BindingService.Annotations["vpn/hostname"] != "infer-abc" || expanded.BindingService.Annotations["vpn/expose"] != "true" {
		t.Fatalf("Annotations = %v", expanded.BindingService.Annotations)
	}
	if expanded.BindingService.Labels["session"] != "infer-abc" {
		t.Fatalf("Labels = %v", expanded.BindingService.Labels)
	}
	if entry.Address != "{{.Orchestrator}}.ts.net:52789" || entry.BindingService.Annotations["vpn/hostname"] != "{{.Orchestrator}}" {
		t.Fatal("ForOrchestrator mutated the entry")
	}
	fixed, err := (WorkspaceEntry{Address: "pods.lan:52789"}).ForOrchestrator("infer-abc")
	if err != nil || fixed.Address != "pods.lan:52789" {
		t.Fatalf("fixed address = %q, %v", fixed.Address, err)
	}
	typo := WorkspaceEntry{Address: "{{.Pod}}.lan"}
	if _, err := typo.ForOrchestrator("infer-abc"); err == nil {
		t.Fatal("an unknown field expanded")
	}
}
