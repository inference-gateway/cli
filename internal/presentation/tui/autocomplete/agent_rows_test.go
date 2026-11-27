package autocomplete

import (
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestAgentRowsLeadWithTheRole(t *testing.T) {
	if got := agentRowDescription(agentdomain.SubagentInfo{Name: "jeff", Role: "tester", Description: "Tests pages."}); got != "tester · Tests pages." {
		t.Fatalf("description = %q", got)
	}
	if got := agentRowDescription(agentdomain.SubagentInfo{Name: "anna", Description: "Researches."}); got != "Researches." {
		t.Fatalf("description without a role = %q", got)
	}
}
