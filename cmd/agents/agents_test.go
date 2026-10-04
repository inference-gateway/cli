package agents

import (
	"testing"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
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
		{"tag and oci are mutually exclusive", "browser-agent", []string{"--tag", "lightpanda", "--oci", "ghcr.io/org/other:v1"}, "", true},
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
