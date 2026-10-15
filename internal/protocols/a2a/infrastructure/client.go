package infrastructure

import (
	"os"

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
	setEntryHeaders(cfg, agentURL)
	return client.NewClientWithConfig(cfg)
}

// setEntryHeaders copies the agent's agents.yaml headers onto the client,
// with $VARS expanded the way channels.yaml values are. A bearer token here is
// only a shorthand: an auth block sets Authorization after these, so its token
// wins when both are configured.
func setEntryHeaders(cfg *client.Config, agentURL string) {
	agent, ok := configuredAgent(agentURL)
	if !ok {
		return
	}
	for name, value := range agent.Headers {
		cfg.Headers[name] = os.ExpandEnv(value)
	}
}
