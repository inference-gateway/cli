package daemon

import (
	"bytes"
	"strings"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestPrintWorkspacesListsTheProtocol(t *testing.T) {
	var out bytes.Buffer
	frame := map[string]any{"workspaces": []agentdomain.WorkspaceStatus{
		{Name: "a", Kind: agentdomain.WorkspaceKindLocal, Protocol: "agui", Reachable: true},
		{Name: "b", Compute: "box", Kind: agentdomain.WorkspaceKindDaemon, Protocol: "a2a", Reachable: true,
			Capabilities: map[string]string{"version": "1.0"}},
		{Name: "ghost", Kind: agentdomain.WorkspaceKindDaemon, Protocol: "agui", Error: "refused"},
	}}
	if err := printWorkspaces(&out, frame); err != nil {
		t.Fatalf("printWorkspaces() = %v", err)
	}
	got := make([]string, 0, 4)
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		got = append(got, strings.TrimRight(line, " "))
	}
	want := []string{
		"NAME   COMPUTE  KIND    PROTOCOL  REACHABLE  DETAILS",
		"a      -        local   agui      true",
		"b      box      daemon  a2a       true       version=1.0",
		"ghost  -        daemon  agui      false      refused",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("printWorkspaces() rendered\n%q\nwant\n%q", got, want)
	}
}
