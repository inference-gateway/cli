package application

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
	containerruntime "github.com/inference-gateway/cli/internal/platform/container"
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

func TestSharedAgentsReachTheGatewayThroughTheHost(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.URL = "http://localhost:8080"
	cfg.Gateway.OCI = "ghcr.io/inference-gateway/inference-gateway:latest"

	shared := NewAgentManager(containerruntime.SharedSessionID, cfg, config.DefaultAgentsConfig(), nil, nil)
	if got := shared.determineGatewayURL(); got != "http://host.docker.internal:8080/v1" {
		t.Fatalf("shared gateway URL = %q", got)
	}
	if got := sharedAgentContainerName("runner"); got != "inference-agent-runner-shared" {
		t.Fatalf("shared container name = %q", got)
	}
}
