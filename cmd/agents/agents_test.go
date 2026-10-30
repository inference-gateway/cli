package agents

import (
	"maps"
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

func findAgentsSubcommand(t *testing.T, name string) *cobra.Command {
	t.Helper()
	command, _, err := NewCommand(runtime.NewState(), output.NewRenderer()).Find([]string{name})
	if err != nil {
		t.Fatalf("find %s command: %v", name, err)
	}
	return command
}

func TestAgentTagAndOCIAreMutuallyExclusive(t *testing.T) {
	for _, name := range []string{"add", "update"} {
		t.Run(name, func(t *testing.T) {
			command := findAgentsSubcommand(t, name)
			if err := command.ParseFlags([]string{"--tag", "lightpanda", "--oci", "ghcr.io/org/other:v1"}); err != nil {
				t.Fatalf("parse flags: %v", err)
			}
			if err := command.ValidateFlagGroups(); err == nil {
				t.Error("expected --tag and --oci to be rejected together")
			}
		})
	}
}

func TestAgentUpdateRequiresAtLeastOneFlag(t *testing.T) {
	if err := findAgentsSubcommand(t, "update").ValidateFlagGroups(); err == nil {
		t.Error("expected update without any flag to be rejected")
	}
}

func TestAgentEnvironmentFlagForms(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{"comma separated", []string{"--environment", "A=1,B=2"}, map[string]string{"A": "1", "B": "2"}},
		{"repeated flag", []string{"--environment", "A=1", "--environment", "B=2"}, map[string]string{"A": "1", "B": "2"}},
		{"value with a comma", []string{"--environment", "HOSTS=a,b"}, map[string]string{"HOSTS": "a,b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := findAgentsSubcommand(t, "add")
			if err := command.ParseFlags(tt.args); err != nil {
				t.Fatalf("parse flags: %v", err)
			}
			got, err := command.Flags().GetStringToString("environment")
			if err != nil {
				t.Fatalf("get environment: %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("environment = %v, want %v", got, tt.want)
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
