package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	mcp "github.com/metoro-io/mcp-golang"
	mcphttp "github.com/metoro-io/mcp-golang/transport/http"
)

// Compile-time interface checks
var (
	_ domain.MCPClient  = (*mcpClient)(nil)
	_ domain.MCPManager = (*MCPManager)(nil)
)

// mcpClient wraps a single MCP server connection with an initialized MCP library client
type mcpClient struct {
	serverName    string
	client        *mcp.Client
	globalConfig  *config.MCPConfig
	serverConfig  config.MCPServerEntry
	mu            sync.RWMutex
	isConnected   bool
	isInitialized bool
}

// newMCPClient creates and initializes a new MCP client for a specific server
func newMCPClient(serverConfig config.MCPServerEntry, globalConfig *config.MCPConfig) *mcpClient {
	transport := mcphttp.NewHTTPClientTransport(serverConfig.URL)

	client := mcp.NewClientWithInfo(transport, mcp.ClientInfo{
		Name:    "inference-gateway-cli",
		Version: "1.0.0",
	})

	return &mcpClient{
		serverName:   serverConfig.Name,
		client:       client,
		globalConfig: globalConfig,
		serverConfig: serverConfig,
		isConnected:  false,
	}
}

// DiscoverTools discovers tools from this MCP server
func (c *mcpClient) DiscoverTools(ctx context.Context) (map[string][]domain.MCPDiscoveredTool, error) {
	timeout := time.Duration(c.serverConfig.GetTimeout(c.globalConfig.ConnectionTimeout)) * time.Second
	serverCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c.mu.Lock()
	if !c.isInitialized {
		_, err := c.client.Initialize(serverCtx)
		if err != nil {
			c.isConnected = false
			c.mu.Unlock()
			return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
		}
		c.isInitialized = true
	}
	c.mu.Unlock()

	toolsResp, err := c.client.ListTools(serverCtx, nil)
	if err != nil {
		c.mu.Lock()
		c.isConnected = false
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	tools := make([]domain.MCPDiscoveredTool, 0, len(toolsResp.Tools))
	for _, tool := range toolsResp.Tools {
		description := ""
		if tool.Description != nil {
			description = *tool.Description
		}

		tools = append(tools, domain.MCPDiscoveredTool{
			ServerName:  c.serverName,
			Name:        tool.Name,
			Description: description,
			InputSchema: tool.InputSchema,
		})
	}

	result := make(map[string][]domain.MCPDiscoveredTool)
	result[c.serverName] = tools

	c.mu.Lock()
	c.isConnected = true
	c.mu.Unlock()

	return result, nil
}

// CallTool executes a tool on this MCP server
func (c *mcpClient) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error) {
	if serverName != c.serverName {
		return nil, fmt.Errorf("server name mismatch: expected %q, got %q", c.serverName, serverName)
	}

	timeout := time.Duration(c.serverConfig.GetTimeout(c.globalConfig.ConnectionTimeout)) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c.mu.Lock()
	if !c.isInitialized {
		_, err := c.client.Initialize(execCtx)
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
		}
		c.isInitialized = true
	}
	c.mu.Unlock()

	result, err := c.client.CallTool(execCtx, toolName, args)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool %q: %w", toolName, err)
	}

	return result, nil
}

// PingServer pings this MCP server
func (c *mcpClient) PingServer(ctx context.Context, serverName string) error {
	if serverName != c.serverName {
		return fmt.Errorf("server name mismatch: expected %q, got %q", c.serverName, serverName)
	}

	timeout := time.Duration(c.serverConfig.GetTimeout(c.globalConfig.ConnectionTimeout)) * time.Second
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c.mu.Lock()
	if !c.isInitialized {
		_, err := c.client.Initialize(pingCtx)
		if err != nil {
			c.isConnected = false
			c.mu.Unlock()
			return fmt.Errorf("failed to initialize MCP client: %w", err)
		}
		c.isInitialized = true
	}
	c.mu.Unlock()

	err := c.client.Ping(pingCtx)
	if err != nil {
		c.mu.Lock()
		c.isConnected = false
		c.mu.Unlock()
		return fmt.Errorf("ping failed: %w", err)
	}

	c.mu.Lock()
	c.isConnected = true
	c.mu.Unlock()

	return nil
}

// Close cleans up resources for this client
func (c *mcpClient) Close() error {
	// No persistent connection to close in stateless HTTP implementation
	return nil
}

// MCPManager manages multiple MCP server connections
type MCPManager struct {
	config         *config.MCPConfig
	mu             sync.RWMutex
	clients        map[string]*mcpClient
	toolCounts     map[string]int
	probeCancel    context.CancelFunc
	probeWg        sync.WaitGroup
	statusChan     chan domain.MCPServerStatusUpdateEvent
	monitorStarted bool
	channelClosed  bool
}

// NewMCPManager creates a new MCP manager
func NewMCPManager(cfg *config.MCPConfig) *MCPManager {
	clients := make(map[string]*mcpClient)

	for _, server := range cfg.Servers {
		if server.Enabled {
			clients[server.Name] = newMCPClient(server, cfg)
		}
	}

	return &MCPManager{
		config:     cfg,
		clients:    clients,
		toolCounts: make(map[string]int),
	}
}

// GetClients returns a list of MCP clients
func (m *MCPManager) GetClients() []domain.MCPClient {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clients := make([]domain.MCPClient, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	return clients
}

// GetTotalServers returns the total number of configured MCP servers from config
func (m *MCPManager) GetTotalServers() int {
	return len(m.config.Servers)
}

// StartMonitoring begins background health monitoring and returns a channel for status updates
// This method is idempotent - calling it multiple times returns the same channel
func (m *MCPManager) StartMonitoring(ctx context.Context) <-chan domain.MCPServerStatusUpdateEvent {
	m.mu.Lock()

	if m.monitorStarted {
		m.mu.Unlock()
		return m.statusChan
	}

	m.statusChan = make(chan domain.MCPServerStatusUpdateEvent, 10)
	m.monitorStarted = true
	m.mu.Unlock()

	m.sendInitialStatusUpdate()

	m.mu.Lock()
	if !m.config.LivenessProbeEnabled {
		close(m.statusChan)
		m.channelClosed = true
		m.mu.Unlock()
		return m.statusChan
	}

	interval := time.Duration(m.config.LivenessProbeInterval) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}

	probeCtx, cancel := context.WithCancel(ctx)
	m.probeCancel = cancel

	logger.Info("Starting MCP liveness probes", "interval", interval, "client_count", len(m.clients))
	for _, client := range m.clients {
		m.probeWg.Add(1)
		go func(c *mcpClient) {
			defer m.probeWg.Done()

			m.checkClientHealth(probeCtx, c)

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-probeCtx.Done():
					return
				case <-ticker.C:
					m.checkClientHealth(probeCtx, c)
				}
			}
		}(client)
	}
	m.mu.Unlock()

	return m.statusChan
}

// checkClientHealth performs a health check on a client and handles reconnection
func (m *MCPManager) checkClientHealth(ctx context.Context, client *mcpClient) {
	client.mu.RLock()
	wasConnected := client.isConnected
	client.mu.RUnlock()

	err := client.PingServer(ctx, client.serverName)
	if err != nil {
		if wasConnected {
			logger.Warn("MCP server became unhealthy", "server", client.serverName, "error", err)
			m.sendStatusUpdate(client.serverName, false)
		}
		return
	}

	if !wasConnected {
		toolsMap, err := client.DiscoverTools(ctx)
		if err != nil {
			logger.Warn("MCP server responded to ping but tool discovery failed",
				"server", client.serverName,
				"error", err)
			return
		}

		tools := toolsMap[client.serverName]

		m.mu.Lock()
		m.toolCounts[client.serverName] = len(tools)
		m.mu.Unlock()

		m.sendStatusUpdateWithTools(client.serverName, true, tools)
	}
}

// sendInitialStatusUpdate sends the current status for all connected clients
func (m *MCPManager) sendInitialStatusUpdate() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, client := range m.clients {
		client.mu.RLock()
		isConnected := client.isConnected
		client.mu.RUnlock()

		if isConnected {
			m.sendStatusUpdateWithTools(client.serverName, true, nil)
		}
	}
}

// sendStatusUpdate sends a status update event to the channel without tools
func (m *MCPManager) sendStatusUpdate(serverName string, connected bool) {
	if !connected {
		m.mu.Lock()
		delete(m.toolCounts, serverName)
		m.mu.Unlock()
	}
	m.sendStatusUpdateWithTools(serverName, connected, nil)
}

// getMCPServerStatus calculates the current MCP server status
func (m *MCPManager) getMCPServerStatus() domain.MCPServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalServers := len(m.config.Servers)
	connectedServers := 0
	totalTools := 0

	for _, client := range m.clients {
		client.mu.RLock()
		if client.isConnected {
			connectedServers++
		}
		client.mu.RUnlock()
	}

	for _, count := range m.toolCounts {
		totalTools += count
	}

	return domain.MCPServerStatus{
		TotalServers:     totalServers,
		ConnectedServers: connectedServers,
		TotalTools:       totalTools,
	}
}

// sendStatusUpdateWithTools sends a status update event with discovered tools
func (m *MCPManager) sendStatusUpdateWithTools(serverName string, connected bool, tools []domain.MCPDiscoveredTool) {
	if m.statusChan == nil {
		logger.Warn("Cannot send status update: status channel is nil", "server", serverName)
		return
	}

	status := m.getMCPServerStatus()

	event := domain.MCPServerStatusUpdateEvent{
		ServerName:       serverName,
		Connected:        connected,
		TotalServers:     status.TotalServers,
		ConnectedServers: status.ConnectedServers,
		TotalTools:       status.TotalTools,
		Tools:            tools,
	}

	select {
	case m.statusChan <- event:
		logger.Debug("MCP status update sent successfully", "server", serverName)
	default:
		logger.Warn("MCP status channel full, skipping update", "server", serverName)
	}
}

// UpdateToolCount updates the tool count for a specific server
func (m *MCPManager) UpdateToolCount(serverName string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCounts[serverName] = count
}

// ClearToolCount removes the tool count for a specific server
func (m *MCPManager) ClearToolCount(serverName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.toolCounts, serverName)
}

// Close stops monitoring and cleans up resources
func (m *MCPManager) Close() error {
	m.mu.Lock()
	if m.probeCancel != nil {
		m.probeCancel()
		m.probeCancel = nil
	}
	m.mu.Unlock()

	m.probeWg.Wait()

	m.mu.Lock()
	if m.statusChan != nil && !m.channelClosed {
		close(m.statusChan)
		m.channelClosed = true
	}
	m.statusChan = nil
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, client := range m.clients {
		_ = client.Close()
	}

	return nil
}
