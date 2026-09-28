package infrastructure

import (
	client "github.com/inference-gateway/adk/client"

	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// NewClient returns an ADK client for the agent at agentURL whose requests
// carry the session's trace context, so remote spans nest under ours.
func NewClient(agentURL string) client.A2AClient {
	cfg := client.DefaultConfig(agentURL)
	cfg.Transport = telemetry.PropagationTransport(nil)
	return client.NewClientWithConfig(cfg)
}
