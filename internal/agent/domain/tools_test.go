package domain

import "testing"

func TestAgentLabelNamesTheRoleInBrackets(t *testing.T) {
	if got := AgentLabel("jeff", "tester"); got != "jeff (tester)" {
		t.Fatalf("label = %q", got)
	}
	if got := (SubagentInfo{Name: "anna"}).Label(); got != "anna" {
		t.Fatalf("label without a role = %q", got)
	}
}
