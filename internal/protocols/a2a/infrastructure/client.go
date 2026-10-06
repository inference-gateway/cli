package infrastructure

import (
	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"

	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// NewClient returns an ADK client for the agent at agentURL whose requests
// carry the session's trace context, so remote spans nest under ours, and the
// credentials agents.yaml gives the agent. They go to the host the agent
// actually runs on. Every request activates the usage extension.
func NewClient(agentURL string) client.A2AClient {
	cfg := client.DefaultConfig(runningURL(agentURL))
	cfg.Transport = telemetry.PropagationTransport(newAuthTransport(agentURL))
	cfg.Headers["A2A-Extensions"] = adk.UsageExtensionURI
	return client.NewClientWithConfig(cfg)
}
