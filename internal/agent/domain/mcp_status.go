package domain

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
