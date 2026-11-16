package sandboxinfra

import (
	"os"
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestCommand_ReexecsWithWritePaths(t *testing.T) {
	cmd := Command(t.Context(), config.DefaultConfig(), agentdomain.AgentModeStandard, "echo hi")

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[0] != exe || cmd.Args[1] != ReexecCommand || !slices.Contains(cmd.Args, "--write") {
		t.Fatalf("Command args = %q, want %s %s --write <dir>... -- bash -c 'echo hi'", cmd.Args, exe, ReexecCommand)
	}
	if tail, want := cmd.Args[len(cmd.Args)-4:], []string{"--", "bash", "-c", "echo hi"}; !slices.Equal(tail, want) {
		t.Fatalf("Command args end with %q, want %q", tail, want)
	}
}
