package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestComputeConfigValidate(t *testing.T) {
	cluster := ClusterEntry{Name: "k3d", Namespace: "agents"}
	machine := MachineEntry{Name: "box", Address: "box.lan:52789"}
	tests := []struct {
		name    string
		compute ComputeConfig
		wantErr string
	}{
		{name: "a cluster and a machine", compute: ComputeConfig{Clusters: []ClusterEntry{cluster}, Machines: []MachineEntry{machine}}},
		{name: "a cluster needs a name", compute: ComputeConfig{Clusters: []ClusterEntry{{Namespace: "agents"}}}, wantErr: "every cluster needs a name"},
		{name: "a cluster needs a namespace", compute: ComputeConfig{Clusters: []ClusterEntry{{Name: "k3d"}}}, wantErr: `cluster "k3d" needs a namespace`},
		{name: "a cluster mode is standard or all-in-one", compute: ComputeConfig{Clusters: []ClusterEntry{{Name: "k3d", Namespace: "agents", Mode: "sidecar"}}}, wantErr: "invalid mode"},
		{name: "a machine needs an address", compute: ComputeConfig{Machines: []MachineEntry{{Name: "box"}}}, wantErr: `machine "box" needs an address`},
		{
			name:    "a cluster and a machine never share a name",
			compute: ComputeConfig{Clusters: []ClusterEntry{cluster}, Machines: []MachineEntry{{Name: "k3d", Address: "a:1"}}},
			wantErr: `compute "k3d" is defined twice`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.compute.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveCompute(t *testing.T) {
	compute := ComputeConfig{
		Clusters: []ClusterEntry{{Name: "k3d", Context: "k3d-agents", Namespace: "agents", Mode: WorkspaceModeStandard, GatewayEnv: []string{"ANTHROPIC_API_KEY"}}},
		Machines: []MachineEntry{{Name: "box", Address: "box.lan:52789", Token: "tok", TLS: PeerTLSConfig{CAFile: "/ca.crt"}}},
	}
	tests := []struct {
		name      string
		workspace WorkspaceEntry
		want      WorkspaceEntry
		wantErr   string
	}{
		{
			name:      "a workspace on a cluster takes its kind, connection and gateway",
			workspace: WorkspaceEntry{Name: "reviewer", Compute: "k3d", Image: "infer:1", Secrets: []string{"reviewer-mail"}},
			want: WorkspaceEntry{
				Name: "reviewer", Compute: "k3d", Kind: WorkspaceKindKubernetes, Context: "k3d-agents", Namespace: "agents",
				Mode: WorkspaceModeStandard, GatewayEnv: []string{"ANTHROPIC_API_KEY"}, Image: "infer:1", Secrets: []string{"reviewer-mail"},
			},
		},
		{
			name:      "a workspace on a machine takes its kind and connection",
			workspace: WorkspaceEntry{Name: "builder", Compute: "box", Protocol: WorkspaceProtocolA2A},
			want: WorkspaceEntry{
				Name: "builder", Compute: "box", Kind: WorkspaceKindDaemon, Address: "box.lan:52789", Token: "tok",
				TLS: PeerTLSConfig{CAFile: "/ca.crt"}, Protocol: WorkspaceProtocolA2A,
			},
		},
		{
			name:      "a workspace with its own connection is left alone",
			workspace: WorkspaceEntry{Name: "legacy", Kind: WorkspaceKindKubernetes, Namespace: "infer", Image: "infer:1"},
			want:      WorkspaceEntry{Name: "legacy", Kind: WorkspaceKindKubernetes, Namespace: "infer", Image: "infer:1"},
		},
		{name: "unknown compute is refused", workspace: WorkspaceEntry{Name: "lost", Compute: "ghost"}, wantErr: `runs on compute "ghost", which compute.yaml does not define`},
		{name: "the compute decides the kind", workspace: WorkspaceEntry{Name: "w", Compute: "k3d", Kind: WorkspaceKindKubernetes}, wantErr: "which decides its kind"},
		{name: "a workspace on a cluster may not repeat its namespace", workspace: WorkspaceEntry{Name: "w", Compute: "k3d", Namespace: "other"}, wantErr: `runs on cluster "k3d", which sets`},
		{name: "a workspace on a machine may not repeat its address", workspace: WorkspaceEntry{Name: "w", Compute: "box", Address: "a:1"}, wantErr: `runs on machine "box", which sets`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon := DaemonConfig{Workspaces: []WorkspaceEntry{tt.workspace}}
			err := daemon.ResolveCompute(compute)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ResolveCompute() = %v, want an error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := daemon.Workspaces[0]; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("resolved %+v, want %+v", got, tt.want)
			}
			if err := daemon.Validate(); err != nil {
				t.Fatalf("a resolved workspace fails Validate() = %v", err)
			}
		})
	}
}

func TestResolvedWorkspacesOwnTheirGatewayEnv(t *testing.T) {
	compute := ComputeConfig{Clusters: []ClusterEntry{{Name: "k3d", Namespace: "agents", GatewayEnv: []string{"ANTHROPIC_API_KEY"}}}}
	daemon := DaemonConfig{Workspaces: []WorkspaceEntry{{Name: "a", Compute: "k3d", Image: "i"}, {Name: "b", Compute: "k3d", Image: "i"}}}
	if err := daemon.ResolveCompute(compute); err != nil {
		t.Fatal(err)
	}
	daemon.Workspaces[0].GatewayEnv[0] = "CHANGED"
	if daemon.Workspaces[1].GatewayEnv[0] != "ANTHROPIC_API_KEY" || compute.Clusters[0].GatewayEnv[0] != "ANTHROPIC_API_KEY" {
		t.Fatal("workspaces on one cluster share its gateway_env slice")
	}
}

func TestGPUSettingsPreferComputeYAMLOverTheLegacyBlock(t *testing.T) {
	tests := []struct {
		name       string
		cfg        Config
		want       GPUConfig
		wantLegacy bool
	}{
		{name: "nothing set", cfg: Config{}, want: GPUConfig{}},
		{
			name:       "only the legacy provisioner block",
			cfg:        Config{Provisioner: ProvisionerConfig{GPUType: "A6000", MaxHourly: 1}},
			want:       GPUConfig{GPUType: "A6000", MaxHourly: 1},
			wantLegacy: true,
		},
		{
			name: "compute.yaml wins over the legacy block",
			cfg:  Config{Provisioner: ProvisionerConfig{GPUType: "A6000"}, Compute: ComputeConfig{GPUs: GPUConfig{GPUType: "H100", MaxHourly: 2}}},
			want: GPUConfig{GPUType: "H100", MaxHourly: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, legacy := tt.cfg.GPUSettings()
			if got != tt.want || legacy != tt.wantLegacy {
				t.Fatalf("GPUSettings() = %+v, %v, want %+v, %v", got, legacy, tt.want, tt.wantLegacy)
			}
		})
	}
}

func TestComputeGPUProviderIsRunPod(t *testing.T) {
	if err := (ComputeConfig{GPUs: GPUConfig{Provider: "lambda"}}).Validate(); err == nil || !strings.Contains(err.Error(), "invalid gpus.provider") {
		t.Fatalf("Validate() = %v, want the provider refused", err)
	}
	if err := (ComputeConfig{GPUs: GPUConfig{Provider: GPUProviderRunPod}}).Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestComputeFileKeepsReferencesUnexpanded(t *testing.T) {
	t.Setenv("WORKSTATION_TOKEN", "the-secret")
	path := filepath.Join(t.TempDir(), ComputeFileName)
	written := "machines:\n  - name: workstation\n    address: workstation.lan:52789\n    token: ${WORKSTATION_TOKEN}\n"
	if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadComputeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Machines[0].Token != "${WORKSTATION_TOKEN}" {
		t.Fatalf("loaded token = %q, want the reference as written", cfg.Machines[0].Token)
	}
	cfg.Clusters = append(cfg.Clusters, ClusterEntry{Name: "k3d", Namespace: "agents"})
	if err := SaveComputeFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "the-secret") || !strings.Contains(string(saved), "${WORKSTATION_TOKEN}") {
		t.Fatalf("saved compute.yaml = %q, want the reference kept and the secret absent", saved)
	}

	missing, err := LoadComputeFile(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil || len(missing.Clusters)+len(missing.Machines) != 0 {
		t.Fatalf("a missing compute.yaml loads as %+v, %v, want an empty one", missing, err)
	}
}

func TestComputeEditsKeepTheFileValid(t *testing.T) {
	cfg := &ComputeConfig{}
	if err := cfg.AddCluster(ClusterEntry{Name: "k3d", Namespace: "agents"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddMachine(MachineEntry{Name: "box", Address: "box:1"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddMachine(MachineEntry{Name: "k3d", Address: "a:1"}); err == nil || !strings.Contains(err.Error(), "defined twice") {
		t.Fatalf("AddMachine() of a cluster's name = %v, want it refused", err)
	}
	if err := cfg.AddCluster(ClusterEntry{Name: "no namespace"}); err == nil {
		t.Fatal("AddCluster() took a cluster the file cannot keep")
	}
	if len(cfg.Clusters) != 1 || len(cfg.Machines) != 1 {
		t.Fatalf("a refused change left %+v", cfg)
	}
	if err := cfg.RemoveCluster("k3d"); err != nil || len(cfg.Clusters) != 0 {
		t.Fatalf("RemoveCluster() = %v, left %+v", err, cfg.Clusters)
	}
	if err := cfg.RemoveMachine("ghost"); err == nil {
		t.Fatal("RemoveMachine() of an unknown machine succeeded")
	}
}

func TestClusterNamesAreLabelValues(t *testing.T) {
	for name, ok := range map[string]bool{"k3d-agents": true, "Prod.EU_1": true, "has space": false, "-leading": false, strings.Repeat("a", 64): false} {
		err := (ComputeConfig{Clusters: []ClusterEntry{{Name: name, Namespace: "agents"}}}).Validate()
		if (err == nil) != ok {
			t.Fatalf("cluster name %q: Validate() = %v, want accepted %v", name, err, ok)
		}
	}
}
