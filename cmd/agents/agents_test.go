package agents

import (
	"testing"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
)

func TestResolveTagFlag(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		args     []string
		expected string
		wantErr  bool
	}{
		{"no tag flag is a no-op", "browser-agent", nil, "", false},
		{"tag resolves against the default image", "browser-agent", []string{"--tag", "lightpanda"}, "ghcr.io/inference-gateway/browser-agent:lightpanda", false},
		{"tag on an unknown agent", "code-reviewer", []string{"--tag", "latest"}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("tag", "", "")
			cmd.Flags().String("oci", "", "")
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatalf("ParseFlags(%v) failed: %v", tt.args, err)
			}

			got, err := resolveTagFlag(cmd, tt.agent)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveTagFlag(%q) error = %v, wantErr %v", tt.agent, err, tt.wantErr)
			}
			if got != tt.expected {
				t.Errorf("resolveTagFlag(%q) = %q, want %q", tt.agent, got, tt.expected)
			}
		})
	}
}

// --tag and --oci are declared mutually exclusive through Cobra's flag groups,
// and the update group is one-required, so the rejection happens before RunE.
func TestAgentFlagGroups(t *testing.T) {
	addCmd, _, err := NewCommand(runtime.NewState(), output.NewRenderer()).Find([]string{"add"})
	if err != nil {
		t.Fatalf("find add command: %v", err)
	}
	if err := addCmd.Flags().Set("tag", "lightpanda"); err != nil {
		t.Fatalf("set tag: %v", err)
	}
	if err := addCmd.Flags().Set("oci", "ghcr.io/org/other:v1"); err != nil {
		t.Fatalf("set oci: %v", err)
	}
	if err := addCmd.ValidateFlagGroups(); err == nil {
		t.Error("expected --tag and --oci to be mutually exclusive on the add command")
	}

	updateCmd, _, err := NewCommand(runtime.NewState(), output.NewRenderer()).Find([]string{"update"})
	if err != nil {
		t.Fatalf("find update command: %v", err)
	}
	if err := updateCmd.ValidateFlagGroups(); err == nil {
		t.Error("expected the update flags to be declared one-required")
	}
}

func TestAddAgentWithoutModelInheritsAtStart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := &command{renderer: output.NewRenderer()}

	var err error
	captureStdout(t, func() {
		err = c.addAgent(&cobra.Command{}, "browser-agent", "http://localhost:8083", "", "ghcr.io/inference-gateway/browser-agent:latest", true, "", nil)
	})
	if err != nil {
		t.Fatalf("addAgent() without a model error = %v, want the entry written", err)
	}

	path, err := agentsConfigPath(&cobra.Command{})
	if err != nil {
		t.Fatalf("agentsConfigPath() error = %v", err)
	}
	cfg, err := config.LoadAgents(path)
	if err != nil {
		t.Fatalf("LoadAgents() error = %v", err)
	}
	if len(cfg.Agents) != 1 || !cfg.Agents[0].Run || cfg.Agents[0].Model != "" {
		t.Errorf("agents = %+v, want one locally run entry with no pinned model", cfg.Agents)
	}
}
