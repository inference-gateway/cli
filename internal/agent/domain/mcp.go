// Model Context Protocol ports: client and manager interfaces, discovery and
// configuration DTOs, and the status events pushed through the UI notifier.

package domain

import "context"

// MCPDiscoveredTool represents a tool discovered from an MCP server
type MCPDiscoveredTool struct {
	ServerName  string
	Name        string
	Description string
	InputSchema any
}

// MCPServerEntry represents an MCP server configuration entry
type MCPServerEntry struct {
	Name         string
	URL          string
	Enabled      bool
	Timeout      int
	Description  string
	IncludeTools []string
	ExcludeTools []string
}

// MCPClient handles communication with MCP servers
type MCPClient interface {
	// DiscoverTools discovers all tools from enabled MCP servers
	DiscoverTools(ctx context.Context) (map[string][]MCPDiscoveredTool, error)

	// CallTool executes a tool on an MCP server
	CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error)

	// PingServer sends a ping request to check if a specific server is alive
	PingServer(ctx context.Context, serverName string) error

	// Close cleans up MCP client resources
	Close() error
}

// MCPManager manages the lifecycle, health monitoring, and container orchestration of MCP servers
type MCPManager interface {
	// Returns a list of clients
	GetClients() []MCPClient

	// GetClient returns the client for a specific server by name, or nil if
	// no client is registered for that name. This is the O(1) lookup variant
	// of GetClients and should be preferred when the server name is known -
	// it avoids re-running DiscoverTools across every client just to find
	// the owning one.
	GetClient(serverName string) MCPClient

	// GetTotalServers returns the total number of configured MCP servers
	GetTotalServers() int

	// StartMonitoring begins background health monitoring, pushing every
	// MCPServerStatusUpdateEvent through the UI notifier injected at
	// construction. Idempotent; the initial status is emitted asynchronously.
	StartMonitoring(ctx context.Context)

	// UpdateToolCount updates the tool count for a specific server
	UpdateToolCount(serverName string, count int)

	// ClearToolCount removes the tool count for a specific server
	ClearToolCount(serverName string)

	// Container lifecycle management
	// StartServers starts all MCP servers that have run=true (non-fatal)
	StartServers(ctx context.Context) error

	// StopServers stops all running MCP server containers
	StopServers(ctx context.Context) error

	// Close stops monitoring, stops containers, and cleans up resources
	Close() error
}

// MCPServerStatus is the aggregate connection status the MCP manager reports
// for the whole server set.
type MCPServerStatus struct {
	TotalServers     int `json:"total_servers"`
	ConnectedServers int `json:"connected_servers"`
	TotalTools       int `json:"total_tools"`
}

// MCPServerStatusUpdateEvent is pushed through the UI notifier whenever a
// server connects, disconnects, or changes its tool count.
type MCPServerStatusUpdateEvent struct {
	ServerName       string
	Connected        bool
	TotalServers     int
	ConnectedServers int
	TotalTools       int
	Tools            []MCPDiscoveredTool
}
