package tui

import (
	"testing"

	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
)

func TestAgentReadiness_FollowsReconciledAgents(t *testing.T) {
	s := NewApplicationState()

	s.UpdateAgentStatus("mock-agent", a2adomain.AgentStateStarting, "Queued", "http://localhost:8081", "")
	readiness := s.GetAgentReadiness()
	if readiness == nil || readiness.TotalAgents != 1 {
		t.Fatalf("an agent added to a chat without agents must start the tracking, got %+v", readiness)
	}

	s.UpdateAgentStatus("other", a2adomain.AgentStateReady, "Ready", "http://localhost:8082", "")
	if readiness.TotalAgents != 2 || readiness.ReadyAgents != 1 {
		t.Fatalf("total must grow with added agents, got %d/%d", readiness.ReadyAgents, readiness.TotalAgents)
	}

	s.UpdateAgentStatus("other", a2adomain.AgentStateRemoved, "Removed from agents.yaml", "", "")
	if _, tracked := readiness.Agents["other"]; tracked || readiness.TotalAgents != 1 || readiness.ReadyAgents != 0 {
		t.Fatalf("a removed agent must leave the tracking, got %d/%d %v", readiness.ReadyAgents, readiness.TotalAgents, readiness.Agents)
	}
}
