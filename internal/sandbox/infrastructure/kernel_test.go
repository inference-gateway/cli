package sandboxinfra

import (
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestCommand_UnconfinedWhenEnforceIsOff(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Filesystem.Enforce = false

	cmd := Command(t.Context(), cfg, agentdomain.AgentModeStandard, "echo hi")

	if want := []string{"bash", "-c", "echo hi"}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("Command args = %q, want %q", cmd.Args, want)
	}
}

func TestDeniedHint(t *testing.T) {
	tests := []struct {
		output string
		hinted bool
	}{
		{output: "touch: /Users/x/f: Operation not permitted", hinted: true},
		{output: "mkdir: cannot create directory '/home/x/f': Permission denied", hinted: true},
		{output: "exit status 1", hinted: false},
	}
	for _, tt := range tests {
		if got := DeniedHint(tt.output) != ""; got != tt.hinted {
			t.Errorf("DeniedHint(%q) hinted = %v, want %v", tt.output, got, tt.hinted)
		}
	}
}
