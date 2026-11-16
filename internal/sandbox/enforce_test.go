package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

func TestWritePaths(t *testing.T) {
	t.Cleanup(func() {
		Granted = Grants{}
	})
	project := t.TempDir()
	readOnly := t.TempDir()
	granted := t.TempDir()
	t.Chdir(project)
	t.Setenv("HOME", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Filesystem.Allowed = append(sandboxdomain.Allow(project),
		sandboxdomain.Allowed{Path: readOnly, Access: sandboxdomain.AccessRead})
	Granted.Add(sandboxdomain.Allowed{Path: granted, Access: sandboxdomain.AccessWrite})
	scratchpad := config.ProjectScratchpadDir()
	tmp := filepath.Clean(os.TempDir())

	tests := []struct {
		name     string
		mode     agentdomain.AgentMode
		want     []string
		unwanted []string
	}{
		{
			name:     "standard writes the project, the grants, the temp dir and /dev",
			mode:     agentdomain.AgentModeStandard,
			want:     []string{project, granted, tmp, "/dev"},
			unwanted: []string{readOnly, scratchpad},
		},
		{
			name:     "plan writes the scratchpad and the temp dir, not the project",
			mode:     agentdomain.AgentModePlan,
			want:     []string{scratchpad, tmp, "/dev"},
			unwanted: []string{project, granted, readOnly},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WritePaths(cfg, tt.mode)
			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Errorf("WritePaths = %q, missing %q", got, want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if slices.Contains(got, unwanted) {
					t.Errorf("WritePaths = %q, must not list %q", got, unwanted)
				}
			}
		})
	}
}
