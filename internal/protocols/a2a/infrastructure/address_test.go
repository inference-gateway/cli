package infrastructure

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestNewClient_ReachesAnAgentMovedToAnotherPort(t *testing.T) {
	vacated := httptest.NewServer(nil)
	configuredURL := vacated.URL
	vacated.Close()

	agent := &authRecorder{}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	writeAgentsYAML(t, fmt.Sprintf("agents:\n  - name: research\n    url: %s\n    auth:\n      token_env: RESEARCH_TOKEN\n", configuredURL))
	t.Setenv("RESEARCH_TOKEN", "s3cret")

	RouteAgent("research", configuredURL, srv.URL)
	t.Cleanup(func() { UnrouteAgent("research") })

	if _, err := NewClient(configuredURL).GetAgentCard(t.Context()); err != nil {
		t.Fatalf("request to the routed agent failed: %v", err)
	}
	if got := agent.headers(); len(got) != 1 || got[0] != "Bearer s3cret" {
		t.Fatalf("Authorization headers = %q, want the agent's credentials on its running origin", got)
	}

	UnrouteAgent("research")
	if _, err := NewClient(configuredURL).GetAgentCard(t.Context()); err == nil {
		t.Fatal("an unrouted agent was still reached on its old port")
	}
}
