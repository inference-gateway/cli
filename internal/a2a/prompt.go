package a2a

import (
	"fmt"

	a2adomain "github.com/inference-gateway/cli/internal/a2a/domain"
)

// AgentsPromptSection lists the configured A2A agents for the system prompt,
// so the model knows whom it can delegate to. Empty when there are none.
func AgentsPromptSection(agents a2adomain.AgentCardService) string {
	if agents == nil {
		return ""
	}

	urls := agents.GetConfiguredAgents()
	if len(urls) == 0 {
		return ""
	}

	agentInfo := "\n\nAvailable A2A Agents:\n"
	for _, url := range urls {
		agentInfo += fmt.Sprintf("- %s\n", url)
	}
	agentInfo += "\nYou can delegate tasks to these agents using the " + ToolSubmitTask + " tool."
	return agentInfo
}
