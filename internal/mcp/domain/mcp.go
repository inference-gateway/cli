// Package domain holds the MCP bounded context's contracts: the tools a
// server advertises, the Client port they are called through, and the
// Supervisor port the composition root and the UI consume. It imports only
// the agent/domain shared kernel, whose Tool contract MCP tools are handed to
// the agent as.
package domain

import (
	"context"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Tool is a tool an MCP server advertises in tools/list.
type Tool struct {
	Server      string
	Name        string
	Description string
	InputSchema map[string]any
}

// CallResult is a tools/call result flattened to text. IsError marks a failure
// the tool reported in its result rather than as a JSON-RPC error.
type CallResult struct {
	Content string
	IsError bool
}

// ToolResult is the Data of an MCP tool's agentdomain.ToolExecutionResult.
type ToolResult struct {
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
	Content    string `json:"content"`
	Error      string `json:"error,omitempty"`
}

// Client talks to one MCP server.
type Client interface {
	// Discover checks the server is up and speaks our protocol version. It is
	// the liveness probe.
	Discover(ctx context.Context) error

	// ListTools returns every tool the server advertises.
	ListTools(ctx context.Context) ([]Tool, error)

	// CallTool runs a tool under its bare, un-namespaced name.
	CallTool(ctx context.Context, name string, args map[string]any) (CallResult, error)
}

// Supervisor owns the configured MCP servers: their containers, their
// liveness, and the agent tools built from what they advertise.
type Supervisor interface {
	// GetTotalServers returns the number of configured MCP servers.
	GetTotalServers() int

	// StartMonitoring begins background liveness probes, pushing every
	// ServerStatusUpdateEvent through the UI notifier injected at
	// construction. Idempotent; the initial status is emitted asynchronously.
	StartMonitoring(ctx context.Context)

	// DiscoverTools probes every server once and returns the agent tools of
	// those that answered, keyed by their namespaced name.
	DiscoverTools(ctx context.Context) map[string]agentdomain.Tool

	// StartServers starts all MCP servers that have run=true (non-fatal).
	StartServers(ctx context.Context) error

	// StopServers stops all running MCP server containers.
	StopServers(ctx context.Context) error

	// Close stops monitoring, stops containers, and cleans up resources.
	Close() error
}

// ServerStatus is the aggregate connection status of the whole server set.
type ServerStatus struct {
	TotalServers     int `json:"total_servers"`
	ConnectedServers int `json:"connected_servers"`
	TotalTools       int `json:"total_tools"`
}

// ServerStatusUpdateEvent is pushed through the UI notifier whenever a server
// connects, disconnects, or changes its tool count. Tools holds the agent
// tools of a server that just connected, keyed by namespaced name.
type ServerStatusUpdateEvent struct {
	ServerName       string
	Connected        bool
	TotalServers     int
	ConnectedServers int
	TotalTools       int
	Tools            map[string]agentdomain.Tool
}

// ToolName is the name the LLM sees an MCP server's tool under.
func ToolName(server, tool string) string {
	return ToolPrefix(server) + tool
}

// ToolPrefix namespaces every tool of one server: MCP_<server>_.
func ToolPrefix(server string) string {
	return "MCP_" + server + "_"
}
