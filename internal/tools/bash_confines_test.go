package tools

import (
	"runtime"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// TestBashTool_Confines: the kernel guards what nobody approved. A command the
// allow-list runs unattended is confined, a command that needs approval runs
// as approved once somebody gives it.
func TestBashTool_Confines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no kernel sandbox on windows")
	}
	project := t.TempDir()
	t.Chdir(project)
	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Filesystem.Allowed = sandboxdomain.Allow(project)
	off := config.DefaultConfig()
	off.Tools.Sandbox.Filesystem.Enforce = false

	tests := []struct {
		name    string
		cfg     *config.Config
		command string
		mode    agentdomain.AgentMode
		want    bool
	}{
		{name: "allow-listed inside the sandbox", cfg: cfg, command: "ls -la", mode: agentdomain.AgentModeStandard, want: true},
		{name: "allow-listed but outside needs approval", cfg: cfg, command: "mkdir /etc/x", mode: agentdomain.AgentModeStandard},
		{name: "off the allow-list needs approval", cfg: cfg, command: "make build", mode: agentdomain.AgentModeStandard},
		{name: "auto mode runs everything unattended", cfg: cfg, command: "make build", mode: agentdomain.AgentModeAutoAccept, want: true},
		{name: "enforce off", cfg: off, command: "ls -la", mode: agentdomain.AgentModeStandard},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewBashTool(tt.cfg, nil).confines(tt.command, tt.mode); got != tt.want {
				t.Errorf("confines(%q, %v) = %v, want %v", tt.command, tt.mode, got, tt.want)
			}
		})
	}
}
