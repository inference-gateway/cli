package application

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestProbeAgents(t *testing.T) {
	report := ProbeAgents(context.Background(),
		[]config.AgentEntry{{Name: "local", URL: "http://127.0.0.1:1"}},
		[]string{"http://127.0.0.1:1/ext"},
	)

	if report.TotalAgents != 2 || report.ReadyAgents != 0 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if report.Agents[0].State != "Failed" || report.Agents[0].Error == "" {
		t.Fatalf("unreachable agent should be Failed with an error: %+v", report.Agents[0])
	}
	if report.Agents[1].Name != "127.0.0.1" {
		t.Fatalf("external agent name should derive from host: %+v", report.Agents[1])
	}
}
