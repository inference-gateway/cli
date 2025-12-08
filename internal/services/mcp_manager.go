package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	mcp "github.com/metoro-io/mcp-golang"
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

// newMCPClient creates a new MCP client (without initializing the transport yet)
func newMCPClient(serverConfig config.MCPServerEntry, globalConfig *config.MCPConfig) *mcpClient {
	return &mcpClient{
		serverName:   serverConfig.Name,
		globalConfig: globalConfig,
		serverConfig: serverConfig,
		isConnected:  false,
	}
}

// initializeClient creates the actual MCP client with the given URL
func (c *mcpClient) initializeClient(serverURL string) {
	transport := NewSSEHTTPClientTransport(serverURL).
		WithHeader("Accept", "application/json, text/event-stream")

	c.client = mcp.NewClientWithInfo(transport, mcp.ClientInfo{
		Name:    "inference-gateway-cli",
		Version: "1.0.0",
	})
	logger.Debug("Initialized MCP client", "server", c.serverName, "url", serverURL)
}

// DiscoverTools discovers tools from this MCP server
func (c *mcpClient) DiscoverTools(ctx context.Context) (map[string][]domain.MCPDiscoveredTool, error) {
	c.mu.Lock()
	if c.client == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("MCP client not initialized yet (container may still be starting)")
	}
	c.mu.Unlock()

	timeout := time.Duration(c.serverConfig.GetTimeout(c.globalConfig.ConnectionTimeout)) * time.Second
	serverCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	logger.Info("Attempting to discover tools from MCP server",
		"server", c.serverName,
		"url", c.serverConfig.GetURL(),
		"timeout", timeout)

	c.mu.Lock()
	if !c.isInitialized {
		logger.Info("Initializing MCP client", "server", c.serverName)
		initResp, err := c.client.Initialize(serverCtx)
		if err != nil {
			c.isConnected = false
			c.mu.Unlock()
			logger.Error("Failed to initialize MCP client",
				"server", c.serverName,
				"error", err)
			return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
		}
		c.isInitialized = true
		logger.Info("MCP client initialized successfully",
			"server", c.serverName,
			"serverInfo", initResp.ServerInfo)
	}
	c.mu.Unlock()

	logger.Info("Listing tools from MCP server", "server", c.serverName)
	toolsResp, err := c.client.ListTools(serverCtx, nil)
	if err != nil {
		c.mu.Lock()
		c.isConnected = false
		c.mu.Unlock()
		logger.Error("Failed to list tools from MCP server",
			"server", c.serverName,
			"error", err)
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	logger.Info("Successfully retrieved tools from MCP server",
		"server", c.serverName,
		"toolCount", len(toolsResp.Tools))

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

	c.mu.Lock()
	if c.client == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("MCP client not initialized yet (container may still be starting)")
	}
	c.mu.Unlock()

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

	c.mu.Lock()
	if c.client == nil {
		c.mu.Unlock()
		return fmt.Errorf("MCP client not initialized yet (container may still be starting)")
	}
	c.mu.Unlock()

	timeout := time.Duration(c.serverConfig.GetTimeout(c.globalConfig.ConnectionTimeout)) * time.Second
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	logger.Debug("Pinging MCP server", "server", c.serverName, "url", c.serverConfig.GetURL())

	c.mu.Lock()
	if !c.isInitialized {
		logger.Debug("Initializing MCP client for ping", "server", c.serverName)
		_, err := c.client.Initialize(pingCtx)
		if err != nil {
			c.isConnected = false
			c.mu.Unlock()
			logger.Warn("Failed to initialize MCP client during ping",
				"server", c.serverName,
				"error", err)
			return fmt.Errorf("failed to initialize MCP client: %w", err)
		}
		c.isInitialized = true
		logger.Debug("MCP client initialized successfully for ping", "server", c.serverName)
	}
	c.mu.Unlock()

	err := c.client.Ping(pingCtx)
	if err != nil {
		c.mu.Lock()
		c.isConnected = false
		c.mu.Unlock()
		logger.Warn("MCP server ping failed",
			"server", c.serverName,
			"error", err)
		return fmt.Errorf("ping failed: %w", err)
	}

	logger.Debug("MCP server ping successful", "server", c.serverName)

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

// MCPManager manages multiple MCP server connections and their container lifecycle
type MCPManager struct {
	sessionID        domain.SessionID
	config           *config.MCPConfig
	containerRuntime domain.ContainerRuntime
	mu               sync.RWMutex
	clients          map[string]*mcpClient
	toolCounts       map[string]int
	probeCancel      context.CancelFunc
	probeWg          sync.WaitGroup
	statusChan       chan domain.MCPServerStatusUpdateEvent
	monitorStarted   bool
	channelClosed    bool
	containerIDs     map[string]string
	assignedPorts    map[string]int
}

// NewMCPManager creates a new MCP manager
func NewMCPManager(sessionID domain.SessionID, cfg *config.MCPConfig, runtime domain.ContainerRuntime) *MCPManager {
	clients := make(map[string]*mcpClient)

	for _, server := range cfg.Servers {
		if server.Enabled {
			clients[server.Name] = newMCPClient(server, cfg)
		}
	}

	return &MCPManager{
		sessionID:        sessionID,
		config:           cfg,
		containerRuntime: runtime,
		clients:          clients,
		toolCounts:       make(map[string]int),
		containerIDs:     make(map[string]string),
		assignedPorts:    make(map[string]int),
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

	if !wasConnected {
		toolsMap, err := client.DiscoverTools(ctx)
		if err != nil {
			logger.Error("MCP server health check failed (tool discovery failed)",
				"server", client.serverName,
				"error", err)
			return
		}

		tools := toolsMap[client.serverName]

		m.mu.Lock()
		m.toolCounts[client.serverName] = len(tools)
		m.mu.Unlock()

		logger.Info("MCP server tools discovered successfully",
			"server", client.serverName,
			"toolCount", len(tools))

		m.sendStatusUpdateWithTools(client.serverName, true, tools)
	} else {
		err := client.PingServer(ctx, client.serverName)
		if err != nil {
			logger.Warn("MCP server became unhealthy", "server", client.serverName, "error", err)
			m.sendStatusUpdate(client.serverName, false)
			return
		}
		logger.Debug("MCP server health check passed", "server", client.serverName)
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

// Close stops monitoring, stops containers, and cleans up resources
func (m *MCPManager) Close() error {
	ctx := context.Background()
	if err := m.StopServers(ctx); err != nil {
		logger.Warn("Failed to stop MCP servers during close", "session", m.sessionID, "error", err)
	}

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
	for _, client := range m.clients {
		if err := client.Close(); err != nil {
			logger.Warn("Failed to close MCP client", "session", m.sessionID, "error", err)
		}
	}
	m.mu.RUnlock()

	if m.containerRuntime != nil {
		if err := m.containerRuntime.CleanupNetwork(ctx); err != nil {
			logger.Warn("Failed to cleanup network during MCP manager close", "session", m.sessionID, "error", err)
		}
	}

	return nil
}

// ==================== Container Lifecycle Management ====================

// StartServers starts all MCP servers that have run=true
// This method is non-fatal and always returns nil
func (m *MCPManager) StartServers(ctx context.Context) error {
	serversToStart := make([]config.MCPServerEntry, 0)
	for _, server := range m.config.Servers {
		if server.Run && server.Enabled {
			serversToStart = append(serversToStart, server)
		}
	}

	if len(serversToStart) == 0 {
		return nil
	}

	if m.containerRuntime != nil {
		if err := m.containerRuntime.EnsureNetwork(ctx); err != nil {
			logger.Warn("Failed to create Docker network", "session", m.sessionID, "error", err)
		}
	}

	var wg sync.WaitGroup
	for _, server := range serversToStart {
		wg.Add(1)
		go func(srv config.MCPServerEntry) {
			defer wg.Done()
			if err := m.StartServer(ctx, srv); err != nil {
				logger.Warn("Failed to start MCP server",
					"session", m.sessionID,
					"server", srv.Name,
					"error", err,
					"container", fmt.Sprintf("inference-mcp-%s-%s", srv.Name, m.sessionID))
			}
		}(server)
	}

	wg.Wait()
	return nil
}

// StartServer starts a single MCP server container
func (m *MCPManager) StartServer(ctx context.Context, server config.MCPServerEntry) error {
	containerName := fmt.Sprintf("inference-mcp-%s-%s", server.Name, m.sessionID)

	assignedPort := m.assignPort(server)

	if m.isServerRunning(containerName) {
		logger.Info("MCP server container already running", "session", m.sessionID, "server", server.Name, "port", assignedPort)
		return nil
	}

	if err := m.pullImage(ctx, server.OCI); err != nil {
		logger.Warn("Failed to pull image, using cached version", "session", m.sessionID, "image", server.OCI, "error", err)
	}

	logger.Info("Starting MCP server", "session", m.sessionID, "server", server.Name, "port", assignedPort)

	if err := m.startContainer(ctx, server, assignedPort); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	logger.Info("Waiting for MCP server to become ready", "session", m.sessionID, "server", server.Name)

	if err := m.waitForReady(ctx, server, assignedPort); err != nil {
		_ = m.stopContainer(ctx, containerName)
		return fmt.Errorf("server failed to become ready: %w", err)
	}

	fullURL := fmt.Sprintf("http://localhost:%d%s", assignedPort, m.getPath(server))
	logger.Info("MCP server started successfully", "session", m.sessionID, "server", server.Name, "url", fullURL)

	m.mu.Lock()
	if client, exists := m.clients[server.Name]; exists {
		client.initializeClient(fullURL)
	}
	m.mu.Unlock()

	return nil
}

// StopServers stops all running MCP server containers
func (m *MCPManager) StopServers(ctx context.Context) error {
	m.mu.Lock()
	containerNames := make([]string, 0, len(m.containerIDs))
	for k := range m.containerIDs {
		containerNames = append(containerNames, k)
	}
	m.mu.Unlock()

	for _, name := range containerNames {
		if err := m.stopContainer(ctx, name); err != nil {
			logger.Warn("Failed to stop MCP server container", "session", m.sessionID, "container", name, "error", err)
		} else {
			logger.Info("Stopped MCP server container", "session", m.sessionID, "container", name)
		}
		m.mu.Lock()
		delete(m.containerIDs, name)
		m.mu.Unlock()
	}

	return nil
}

// pullImage pulls the container image
func (m *MCPManager) pullImage(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull failed: %w, output: %s", err, string(output))
	}
	return nil
}

// startContainer starts the MCP server container
func (m *MCPManager) startContainer(ctx context.Context, server config.MCPServerEntry, assignedPort int) error {
	containerName := fmt.Sprintf("inference-mcp-%s-%s", server.Name, m.sessionID)

	var networkName string
	if m.containerRuntime != nil {
		networkName = m.containerRuntime.GetNetworkName()
	}
	args := []string{
		"run",
		"-d",
		"--name", containerName,
		"--network", networkName,
		"--rm",
	}

	args = m.appendPortMappings(args, server, assignedPort)

	healthCmd := server.HealthCmd
	if healthCmd == "" {
		healthCmd = `sh -c 'curl -f -X POST http://localhost:3000/mcp -H "Content-Type: application/json" -d "{\"jsonrpc\":\"2.0\",\"method\":\"ping\",\"id\":1}" || exit 1'`
	}
	args = append(args,
		"--health-cmd", healthCmd,
		"--health-interval", "10s",
		"--health-timeout", "5s",
		"--health-retries", "3",
		"--health-start-period", "10s",
	)

	for key, value := range server.Env {
		expandedValue := os.ExpandEnv(value)
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, expandedValue))
	}

	for _, volume := range server.Volumes {
		args = append(args, "-v", volume)
	}

	if len(server.Entrypoint) > 0 {
		args = append(args, "--entrypoint", server.Entrypoint[0])
	}

	args = append(args, server.OCI)

	if len(server.Entrypoint) > 1 {
		args = append(args, server.Entrypoint[1:]...)
	} else if len(server.Command) > 0 {
		args = append(args, server.Command...)
	}

	if len(server.Args) > 0 {
		args = append(args, server.Args...)
	}

	logger.Info("Starting MCP server container",
		"session", m.sessionID,
		"server", server.Name,
		"command", fmt.Sprintf("docker %s", strings.Join(args, " ")))

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run failed: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))
	m.mu.Lock()
	m.containerIDs[containerName] = containerID
	m.mu.Unlock()

	return nil
}

// stopContainer stops and removes a container
func (m *MCPManager) stopContainer(ctx context.Context, containerName string) error {
	if m.containerRuntime != nil && !m.containerRuntime.ContainerExists(containerName) {
		return nil
	}

	cmd := exec.CommandContext(ctx, "docker", "stop", containerName)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to stop container", "session", m.sessionID, "container", containerName, "error", err)
	}

	return nil
}

// isServerRunning checks if a container is already running
func (m *MCPManager) isServerRunning(containerName string) bool {
	cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("name=%s", containerName), "--format", "{{.ID}}\t{{.Names}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			continue
		}
		containerID := parts[0]
		foundName := parts[1]

		if foundName == containerName {
			m.mu.Lock()
			m.containerIDs[containerName] = containerID
			m.mu.Unlock()
			return true
		}
	}
	return false
}

// waitForReady waits for the server to become ready by using Docker's healthcheck status
func (m *MCPManager) waitForReady(ctx context.Context, server config.MCPServerEntry, _ int) error {
	containerName := fmt.Sprintf("inference-mcp-%s-%s", server.Name, m.sessionID)
	timeout := time.Duration(server.GetStartupTimeout()) * time.Second
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for server to become ready")
			}

			cmd := exec.Command("docker", "inspect", "--format", "{{.State.Health.Status}}", containerName)
			output, err := cmd.Output()
			if err != nil {
				continue
			}

			healthStatus := strings.TrimSpace(string(output))
			if healthStatus == "healthy" {
				return nil
			}
			if healthStatus == "unhealthy" {
				return fmt.Errorf("container became unhealthy during startup")
			}
		}
	}
}

// assignPort assigns a port for the server, finding an available one if needed
func (m *MCPManager) assignPort(server config.MCPServerEntry) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if port, exists := m.assignedPorts[server.Name]; exists {
		return port
	}

	port := m.determinePort(server)
	m.assignedPorts[server.Name] = port
	return port
}

// appendPortMappings adds port mappings to docker run args
func (m *MCPManager) appendPortMappings(args []string, server config.MCPServerEntry, assignedPort int) []string {
	if server.Port > 0 {
		return append(args, "-p", fmt.Sprintf("%d:3000", assignedPort))
	}

	if len(server.Ports) > 0 {
		for i, portMapping := range server.Ports {
			mappedPort := m.mapPort(portMapping, i, assignedPort)
			args = append(args, "-p", mappedPort)
		}
		return args
	}

	return append(args, "-p", fmt.Sprintf("%d:8080", assignedPort))
}

// mapPort creates the port mapping string for docker
func (m *MCPManager) mapPort(portMapping string, index int, assignedPort int) string {
	if index != 0 {
		return portMapping
	}

	if strings.Contains(portMapping, ":") {
		parts := strings.Split(portMapping, ":")
		return fmt.Sprintf("%d:%s", assignedPort, parts[1])
	}

	return fmt.Sprintf("%d:%s", assignedPort, portMapping)
}

// determinePort determines the port to assign to a server
func (m *MCPManager) determinePort(server config.MCPServerEntry) int {
	if server.Port > 0 {
		return server.Port
	}

	primaryPort := server.GetPrimaryPort()
	if len(server.Ports) > 0 && primaryPort > 0 {
		return config.FindAvailablePort(primaryPort)
	}

	return config.FindAvailablePort(3000)
}

// getPath returns the path for the server
func (m *MCPManager) getPath(server config.MCPServerEntry) string {
	if server.Path != "" {
		return server.Path
	}
	return "/mcp"
}
