package config

import (
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
