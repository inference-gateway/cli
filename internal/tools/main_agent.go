package tools

import (
	"fmt"
	"slices"
	"strings"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	mcpdomain "github.com/inference-gateway/cli/internal/protocols/mcp/domain"
)

// MainAgentAllows reports whether the main agent a chat switched to may call
// the named tool: not in its disallowedTools, in its tools list when it has
// one, and an MCP tool only from a server its mcpServers names when it has one.
func MainAgentAllows(agent agentdomain.MainAgent, name string) bool {
	if slices.Contains(agent.DisallowedTools, name) {
		return false
	}
	if agent.Tools != nil && !slices.Contains(agent.Tools, name) {
		return false
	}
	if !strings.HasPrefix(name, mcpdomain.ToolNamePrefix) || agent.MCPServers == nil {
		return true
	}
	return slices.ContainsFunc(agent.MCPServers, func(server string) bool {
		return strings.HasPrefix(name, mcpdomain.ToolPrefix(server))
	})
}

// mainAgentToolError tells the model that its main agent may not call a tool,
// naming the ones it may, so it picks one instead of retrying.
func mainAgentToolError(agent agentdomain.MainAgent, name string) error {
	if agent.Tools == nil {
		return fmt.Errorf("tool not allowed: %s is not available to agent %q", name, agent.Name)
	}
	return fmt.Errorf("tool not allowed: %s is not available to agent %q - use one of %s instead",
		name, agent.Name, strings.Join(agent.Tools, ", "))
}
