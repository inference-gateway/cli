package tui

import (
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestCycleAgentMode_FourWay(t *testing.T) {
	s := NewApplicationState()
	want := []agentdomain.AgentMode{
		agentdomain.AgentModePlan,
		agentdomain.AgentModeAutoAccept,
		agentdomain.AgentModeAutoWithJudge,
		agentdomain.AgentModeStandard,
	}
	for i, w := range want {
		if got := s.CycleAgentMode(); got != w {
			t.Fatalf("cycle %d = %v, want %v", i, got, w)
		}
	}
}
