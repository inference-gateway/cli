package mcp

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	mcpdomain "github.com/inference-gateway/cli/internal/mcp/domain"
	mcpinfra "github.com/inference-gateway/cli/internal/mcp/infrastructure"
	containerruntime "github.com/inference-gateway/cli/internal/platform/container"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// Compile-time interface checks
var (
	_ mcpdomain.Client     = (*mcpClient)(nil)
	_ mcpdomain.Supervisor = (*Supervisor)(nil)
)

// mcpClient is one configured MCP server: the connection state the liveness
// probe tracks, and the mcpdomain.Client its tools call through. It bounds
// every call by the server's timeout and names the tools it lists.
type mcpClient struct {
	serverName        string
	rpc               mcpdomain.Client
	globalConfig      *config.MCPConfig
	serverConfig      config.MCPServerEntry
	mu                sync.RWMutex
	isConnected       bool
	retryAttempt      int
	lastAttemptTime   time.Time
	permanentlyFailed bool
}

// newMCPClient creates a new MCP client (without a server URL yet)
func newMCPClient(serverConfig config.MCPServerEntry, globalConfig *config.MCPConfig) *mcpClient {
	return &mcpClient{
		serverName:   serverConfig.Name,
		globalConfig: globalConfig,
		serverConfig: serverConfig,
		isConnected:  false,
	}
}

// initializeClient points the client at serverURL. StartServer calls it once
// a container is up, while probes may be reading the client, so it takes c.mu.
func (c *mcpClient) initializeClient(serverURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rpc = mcpinfra.NewClient(serverURL)
	logger.Debug("initialized MCP client", "server", c.serverName, "url", serverURL)
}

// conn bounds ctx by the server's timeout and returns the server's client.
// The caller must call cancel.
func (c *mcpClient) conn(ctx context.Context) (mcpdomain.Client, context.Context, context.CancelFunc, error) {
	timeout := time.Duration(c.serverConfig.GetTimeout(c.globalConfig.ConnectionTimeout)) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.rpc == nil {
		return nil, ctx, cancel, fmt.Errorf("MCP client not initialized yet (container may still be starting)")
	}
	return c.rpc, ctx, cancel, nil
}

// Discover implements mcpdomain.Client.
func (c *mcpClient) Discover(ctx context.Context) error {
	rpc, ctx, cancel, err := c.conn(ctx)
	defer cancel()
	if err != nil {
		return err
	}
	return rpc.Discover(ctx)
}

// ListTools implements mcpdomain.Client.
func (c *mcpClient) ListTools(ctx context.Context) ([]mcpdomain.Tool, error) {
	rpc, ctx, cancel, err := c.conn(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	tools, err := rpc.ListTools(ctx)
	for i := range tools {
		tools[i].Server = c.serverName
	}
	return tools, err
}

// CallTool implements mcpdomain.Client.
func (c *mcpClient) CallTool(ctx context.Context, name string, args map[string]any) (mcpdomain.CallResult, error) {
	rpc, ctx, cancel, err := c.conn(ctx)
	defer cancel()
	if err != nil {
		return mcpdomain.CallResult{}, err
	}
	return rpc.CallTool(ctx, name, args)
}

// discoverTools checks the server speaks our protocol, lists its tools and
// wraps them for the agent, recording whether the server answered.
func (c *mcpClient) discoverTools(ctx context.Context) (map[string]agentdomain.Tool, error) {
	err := c.Discover(ctx)
	var found []mcpdomain.Tool
	if err == nil {
		found, err = c.ListTools(ctx)
	}
	c.setConnected(err == nil)
	if err != nil {
		return nil, err
	}
	return newTools(found, c, c.globalConfig), nil
}

// probe is the liveness check of a server that already answered once.
func (c *mcpClient) probe(ctx context.Context) error {
	err := c.Discover(ctx)
	c.setConnected(err == nil)
	return err
}

func (c *mcpClient) setConnected(connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isConnected = connected
}

// Supervisor manages multiple MCP server connections and their container lifecycle
type Supervisor struct {
	sessionID        convdomain.SessionID
	config           *config.MCPConfig
	containerRuntime containerruntime.ContainerRuntime
	notifier         agentdomain.UINotifier
	mu               sync.RWMutex
	clients          map[string]*mcpClient
	toolCounts       map[string]int
	probeCancel      context.CancelFunc
	probeWg          sync.WaitGroup
	monitorStarted   bool
	containerIDs     map[string]string
	assignedPorts    map[string]int
}

// NewSupervisor creates a new MCP supervisor. notifier is the single UI ingress the
// liveness probes push ServerStatusUpdateEvent through; a nil notifier
// degrades to no UI pushes.
func NewSupervisor(sessionID convdomain.SessionID, cfg *config.MCPConfig, runtime containerruntime.ContainerRuntime, notifier agentdomain.UINotifier) *Supervisor {
	if notifier == nil {
		notifier = agentdomain.NoopUINotifier{}
	}
	clients := make(map[string]*mcpClient)

	for _, server := range cfg.Servers {
		if server.Enabled {
			client := newMCPClient(server, cfg)
			if !server.Run {
				serverURL := server.GetURL()
				client.initializeClient(serverURL)
			}
			clients[server.Name] = client
		}
	}

	return &Supervisor{
		sessionID:        sessionID,
		config:           cfg,
		containerRuntime: runtime,
		notifier:         notifier,
		clients:          clients,
		toolCounts:       make(map[string]int),
		containerIDs:     make(map[string]string),
		assignedPorts:    make(map[string]int),
	}
}

// notify pushes an event to the UI loop. NewSupervisor defaults a nil notifier to
// NoopUINotifier, so s.notifier is always non-nil here.
func (s *Supervisor) notify(event any) {
	s.notifier.Notify(event)
}

// DiscoverTools probes every server once, in parallel, and returns the tools
// of those that answered. Headless calls it before the first turn, since it has
// no liveness loop to register tools as servers come up.
// ponytail: run:true container servers still starting in the background are
// skipped (their client has no URL yet); wait on StartServers if headless ever
// needs container-hosted MCP tools on the first turn.
func (s *Supervisor) DiscoverTools(ctx context.Context) map[string]agentdomain.Tool {
	s.mu.RLock()
	clients := slices.Collect(maps.Values(s.clients))
	s.mu.RUnlock()

	// Buffered to len(clients) so no sender ever blocks; this function owns
	// the channel and closes it once every sender is done. Each probe is
	// bounded by its server's timeout, so wg.Wait always returns.
	found := make(chan map[string]agentdomain.Tool, len(clients))
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Go(func() {
			tools, err := client.discoverTools(ctx)
			if err != nil {
				logger.Warn("mcp tool discovery failed", "server", client.serverName, "error", err)
				return
			}
			found <- tools
		})
	}
	wg.Wait()
	close(found)

	all := make(map[string]agentdomain.Tool)
	for tools := range found {
		maps.Copy(all, tools)
	}
	return all
}

// GetTotalServers returns the total number of configured MCP servers from config
func (s *Supervisor) GetTotalServers() int {
	return len(s.config.Servers)
}

// StartMonitoring begins background health monitoring, pushing every
// ServerStatusUpdateEvent through the injected UI notifier. It is idempotent -
// subsequent calls are no-ops. The initial status is emitted from a goroutine
// because Notify wraps the unbuffered (*tea.Program).Send, which blocks until the
// Bubble Tea loop consumes; emitting it synchronously from app.Init (before the
// loop runs) would deadlock.
func (s *Supervisor) StartMonitoring(ctx context.Context) {
	s.mu.Lock()
	if s.monitorStarted {
		s.mu.Unlock()
		return
	}
	s.monitorStarted = true
	s.mu.Unlock()

	go s.sendInitialStatusUpdate()

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.config.LivenessProbeEnabled {
		return
	}

	interval := time.Duration(s.config.LivenessProbeInterval) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}

	probeCtx, cancel := context.WithCancel(ctx)
	s.probeCancel = cancel

	logger.Info("starting MCP liveness probes", "interval", interval, "client_count", len(s.clients))
	for _, client := range s.clients {
		s.probeWg.Add(1)
		go func(c *mcpClient) {
			defer s.probeWg.Done()

			s.checkClientHealth(probeCtx, c)

			for {
				select {
				case <-probeCtx.Done():
					return
				default:
					c.mu.RLock()
					isConnected := c.isConnected
					retryAttempt := c.retryAttempt
					isPermanentlyFailed := c.permanentlyFailed
					c.mu.RUnlock()

					if isPermanentlyFailed {
						logger.Info("stopping health monitoring for permanently failed MCP server", "server", c.serverName)
						return
					}

					var delay time.Duration
					if isConnected {
						delay = interval
					} else {
						delay = s.calculateBackoff(retryAttempt, interval)
					}

					select {
					case <-probeCtx.Done():
						return
					case <-time.After(delay):
						s.checkClientHealth(probeCtx, c)
					}
				}
			}
		}(client)
	}
}

// checkClientHealth performs a health check on a client and handles reconnection
func (s *Supervisor) checkClientHealth(ctx context.Context, client *mcpClient) {
	client.mu.RLock()
	wasConnected := client.isConnected
	isPermanentlyFailed := client.permanentlyFailed
	client.mu.RUnlock()

	if isPermanentlyFailed {
		return
	}

	maxRetries := s.config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 10
	}

	if !wasConnected {
		s.handleToolDiscovery(ctx, client, maxRetries)
		return
	}

	s.handlePing(ctx, client, maxRetries)
}

// handleToolDiscovery attempts to discover tools and handles retry logic
func (s *Supervisor) handleToolDiscovery(ctx context.Context, client *mcpClient, maxRetries int) {
	tools, err := client.discoverTools(ctx)
	if err != nil {
		s.handleDiscoveryFailure(client, maxRetries, err)
		return
	}

	client.mu.Lock()
	client.retryAttempt = 0
	client.lastAttemptTime = time.Now()
	client.mu.Unlock()

	s.mu.Lock()
	s.toolCounts[client.serverName] = len(tools)
	s.mu.Unlock()

	logger.Info("mCP server tools discovered successfully",
		"server", client.serverName,
		"toolCount", len(tools))

	s.sendStatusUpdateWithTools(client.serverName, true, tools)
}

// handleDiscoveryFailure handles tool discovery failures and retry logic
func (s *Supervisor) handleDiscoveryFailure(client *mcpClient, maxRetries int, err error) {
	client.mu.Lock()
	client.retryAttempt++
	client.lastAttemptTime = time.Now()
	retryCount := client.retryAttempt

	if retryCount >= maxRetries {
		client.permanentlyFailed = true
		client.mu.Unlock()
		logger.Error("mCP server permanently failed after max retries",
			"server", client.serverName,
			"retryAttempt", retryCount,
			"maxRetries", maxRetries,
			"error", err)
		return
	}
	client.mu.Unlock()

	logger.Debug("mCP server health check failed (tool discovery failed)",
		"server", client.serverName,
		"retryAttempt", retryCount,
		"maxRetries", maxRetries,
		"error", err)
}

// handlePing attempts to ping the server and handles retry logic
func (s *Supervisor) handlePing(ctx context.Context, client *mcpClient, maxRetries int) {
	err := client.probe(ctx)
	if err != nil {
		s.handlePingFailure(client, maxRetries, err)
		return
	}

	client.mu.Lock()
	client.retryAttempt = 0
	client.lastAttemptTime = time.Now()
	client.mu.Unlock()

	logger.Debug("mCP server health check passed", "server", client.serverName)
}

// handlePingFailure handles ping failures and retry logic
func (s *Supervisor) handlePingFailure(client *mcpClient, maxRetries int, err error) {
	client.mu.Lock()
	client.retryAttempt++
	client.lastAttemptTime = time.Now()
	retryCount := client.retryAttempt

	if retryCount >= maxRetries {
		client.permanentlyFailed = true
		client.mu.Unlock()
		logger.Error("mCP server permanently failed after max retries",
			"server", client.serverName,
			"retryAttempt", retryCount,
			"maxRetries", maxRetries,
			"error", err)
		s.sendStatusUpdate(client.serverName, false)
		return
	}
	client.mu.Unlock()

	logger.Warn("mCP server became unhealthy", "server", client.serverName, "error", err)
	s.sendStatusUpdate(client.serverName, false)
}

// calculateBackoff calculates exponential backoff delay
// Formula: min(baseInterval * 2^attempt, maxBackoff)
func (s *Supervisor) calculateBackoff(attempt int, baseInterval time.Duration) time.Duration {
	const maxBackoff = 5 * time.Minute

	if attempt == 0 {
		return baseInterval
	}

	// Calculate 2^attempt with overflow protection
	multiplier := 1 << uint(attempt)
	if multiplier > 32 {
		multiplier = 32 // Cap at 2^5 = 32x
	}

	backoff := baseInterval * time.Duration(multiplier)
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	logger.Debug("calculated exponential backoff",
		"attempt", attempt,
		"baseInterval", baseInterval,
		"backoff", backoff)

	return backoff
}

// sendInitialStatusUpdate pushes the current status for all connected clients. It
// collects the connected server names under the lock, then emits after releasing
// it: notify wraps the blocking program.Send, which must never run under s.mu.
func (s *Supervisor) sendInitialStatusUpdate() {
	s.mu.RLock()
	connected := make([]string, 0, len(s.clients))
	for _, client := range s.clients {
		client.mu.RLock()
		isConnected := client.isConnected
		client.mu.RUnlock()
		if isConnected {
			connected = append(connected, client.serverName)
		}
	}
	s.mu.RUnlock()

	for _, name := range connected {
		s.sendStatusUpdateWithTools(name, true, nil)
	}
}

// sendStatusUpdate sends a status update event to the channel without tools
func (s *Supervisor) sendStatusUpdate(serverName string, connected bool) {
	if !connected {
		s.mu.Lock()
		delete(s.toolCounts, serverName)
		s.mu.Unlock()
	}
	s.sendStatusUpdateWithTools(serverName, connected, nil)
}

// getMCPServerStatus calculates the current MCP server status
func (s *Supervisor) getMCPServerStatus() mcpdomain.ServerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalServers := len(s.config.Servers)
	connectedServers := 0
	totalTools := 0

	for _, client := range s.clients {
		client.mu.RLock()
		if client.isConnected {
			connectedServers++
		}
		client.mu.RUnlock()
	}

	for _, count := range s.toolCounts {
		totalTools += count
	}

	return mcpdomain.ServerStatus{
		TotalServers:     totalServers,
		ConnectedServers: connectedServers,
		TotalTools:       totalTools,
	}
}

// sendStatusUpdateWithTools pushes a status update event with discovered tools
// through the UI notifier. getMCPServerStatus takes and releases s.mu before
// notify runs, so the blocking program.Send is never called under the lock.
func (s *Supervisor) sendStatusUpdateWithTools(serverName string, connected bool, tools map[string]agentdomain.Tool) {
	status := s.getMCPServerStatus()

	s.notify(mcpdomain.ServerStatusUpdateEvent{
		ServerName:       serverName,
		Connected:        connected,
		TotalServers:     status.TotalServers,
		ConnectedServers: status.ConnectedServers,
		TotalTools:       status.TotalTools,
		Tools:            tools,
	})
}

// Close stops monitoring, stops containers, and cleans up resources
func (s *Supervisor) Close() error {
	ctx := context.Background()
	if err := s.StopServers(ctx); err != nil {
		logger.Warn("failed to stop MCP servers during close", "session", s.sessionID, "error", err)
	}

	s.mu.Lock()
	if s.probeCancel != nil {
		s.probeCancel()
		s.probeCancel = nil
	}
	s.mu.Unlock()

	s.probeWg.Wait()

	if s.containerRuntime != nil {
		if err := s.containerRuntime.CleanupNetwork(ctx); err != nil {
			logger.Warn("failed to cleanup network during MCP supervisor close", "session", s.sessionID, "error", err)
		}
	}

	return nil
}

// ==================== Container Lifecycle Management ====================

// StartServers starts all MCP servers that have run=true
// This method is non-fatal and always returns nil
func (s *Supervisor) StartServers(ctx context.Context) error {
	if utils.IsRunningInContainer() {
		logger.Debug("running in container mode - skipping local mcp server startup")
		return nil
	}

	serversToStart := make([]config.MCPServerEntry, 0)
	for _, server := range s.config.Servers {
		if server.Run && server.Enabled {
			serversToStart = append(serversToStart, server)
		}
	}

	if len(serversToStart) == 0 {
		return nil
	}
	if s.containerRuntime == nil {
		logger.Warn("no container runtime configured; skipping run:true MCP servers", "session", s.sessionID)
		return nil
	}

	if err := s.containerRuntime.EnsureNetwork(ctx); err != nil {
		logger.Warn("failed to create container network", "session", s.sessionID, "error", err)
	}

	var wg sync.WaitGroup
	for _, server := range serversToStart {
		wg.Add(1)
		go func(srv config.MCPServerEntry) {
			defer wg.Done()
			if err := s.StartServer(ctx, srv); err != nil {
				logger.Warn("failed to start MCP server",
					"session", s.sessionID,
					"server", srv.Name,
					"error", err,
					"container", fmt.Sprintf("inference-mcp-%s-%s", srv.Name, s.sessionID))
			}
		}(server)
	}

	wg.Wait()
	return nil
}

// StartServer starts a single MCP server container, or points the client at a
// detached shared container when one is already running.
func (s *Supervisor) StartServer(ctx context.Context, server config.MCPServerEntry) error {
	if s.containerRuntime == nil {
		return fmt.Errorf("no container runtime configured to run MCP server %q", server.Name)
	}
	if url, ok := s.sharedServerURL(ctx, server); ok {
		logger.Info("reusing detached MCP server container", "session", s.sessionID, "server", server.Name, "url", url)
		s.mu.Lock()
		if client, exists := s.clients[server.Name]; exists {
			client.initializeClient(url)
		}
		s.mu.Unlock()
		return nil
	}

	containerName := fmt.Sprintf("inference-mcp-%s-%s", server.Name, s.sessionID)

	assignedPort := s.assignPort(server)

	if s.isServerRunning(ctx, containerName) {
		logger.Info("mCP server container already running", "session", s.sessionID, "server", server.Name, "port", assignedPort)
		return nil
	}

	if err := s.containerRuntime.PullImage(ctx, server.OCI, nil); err != nil {
		logger.Warn("failed to pull image, using cached version", "session", s.sessionID, "image", server.OCI, "error", err)
	}

	logger.Info("starting MCP server", "session", s.sessionID, "server", server.Name, "port", assignedPort)

	if err := s.startContainer(ctx, server, assignedPort); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	logger.Info("waiting for MCP server to become ready", "session", s.sessionID, "server", server.Name)

	if err := s.waitForReady(ctx, containerName, server.GetStartupTimeout()); err != nil {
		_ = s.stopContainer(ctx, containerName)
		return fmt.Errorf("server failed to become ready: %w", err)
	}

	fullURL := fmt.Sprintf("http://localhost:%d%s", assignedPort, s.getPath(server))
	logger.Info("mCP server started successfully", "session", s.sessionID, "server", server.Name, "url", fullURL)

	s.mu.Lock()
	if client, exists := s.clients[server.Name]; exists {
		client.initializeClient(fullURL)
	}
	s.mu.Unlock()

	return nil
}

// StopServer stops this session's container for the named server. With the
// shared session id it stops the detached container started by `infer mcp start`.
func (s *Supervisor) StopServer(ctx context.Context, serverName string) error {
	return s.stopContainer(ctx, fmt.Sprintf("inference-mcp-%s-%s", serverName, s.sessionID))
}

// sharedServerURL reports the URL of a running detached container for the
// server, if any. The shared supervisor itself never reuses (it is the one starting).
func (s *Supervisor) sharedServerURL(ctx context.Context, server config.MCPServerEntry) (string, bool) {
	if s.sessionID == containerruntime.SharedSessionID {
		return "", false
	}
	containerName := fmt.Sprintf("inference-mcp-%s-%s", server.Name, containerruntime.SharedSessionID)
	port, err := s.containerRuntime.PublishedPort(ctx, containerName)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("http://localhost:%d%s", port, s.getPath(server)), true
}

// StopServers stops all running MCP server containers
func (s *Supervisor) StopServers(ctx context.Context) error {
	s.mu.Lock()
	containerNames := make([]string, 0, len(s.containerIDs))
	for k := range s.containerIDs {
		containerNames = append(containerNames, k)
	}
	s.mu.Unlock()

	for _, name := range containerNames {
		if err := s.stopContainer(ctx, name); err != nil {
			logger.Warn("failed to stop MCP server container", "session", s.sessionID, "container", name, "error", err)
		} else {
			logger.Info("stopped MCP server container", "session", s.sessionID, "container", name)
		}
		s.mu.Lock()
		delete(s.containerIDs, name)
		s.mu.Unlock()
	}

	return nil
}

// defaultHealthCmd probes a container's server with a 2026-07-28 server/discover.
const defaultHealthCmd = `sh -c 'curl -fsS -X POST http://localhost:3000/mcp` +
	` -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream"` +
	` -H "MCP-Protocol-Version: ` + mcpinfra.ProtocolVersion + `" -H "Mcp-Method: server/discover"` +
	` -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"server/discover\",\"params\":{\"_meta\":{` +
	`\"io.modelcontextprotocol/protocolVersion\":\"` + mcpinfra.ProtocolVersion + `\",` +
	`\"io.modelcontextprotocol/clientCapabilities\":{}}}}" > /dev/null || exit 1'`

// startContainer runs the MCP server container detached, removed on exit.
func (s *Supervisor) startContainer(ctx context.Context, server config.MCPServerEntry, assignedPort int) error {
	containerName := fmt.Sprintf("inference-mcp-%s-%s", server.Name, s.sessionID)

	healthCmd := server.HealthCmd
	if healthCmd == "" {
		healthCmd = defaultHealthCmd
	}

	env := make(map[string]string, len(server.Env))
	for key, value := range server.Env {
		env[key] = os.ExpandEnv(value)
	}

	containerID, err := s.containerRuntime.RunContainer(ctx, containerruntime.RunContainerOptions{
		Name:         containerName,
		Image:        server.OCI,
		Network:      s.containerRuntime.GetNetworkName(),
		Ports:        s.portMappings(server, assignedPort),
		Environment:  env,
		Volumes:      server.Volumes,
		Entrypoint:   server.Entrypoint,
		Command:      server.Command,
		Args:         server.Args,
		HealthCmd:    healthCmd,
		RemoveOnExit: true,
		Detached:     true,
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.containerIDs[containerName] = containerID
	s.mu.Unlock()
	return nil
}

// stopContainer stops a container; failures are logged, never returned, so
// shutdown is not blocked by a container that is already gone.
func (s *Supervisor) stopContainer(ctx context.Context, containerName string) error {
	if s.containerRuntime == nil {
		return nil
	}
	if err := s.containerRuntime.StopContainer(ctx, containerName); err != nil {
		logger.Warn("failed to stop container", "session", s.sessionID, "container", containerName, "error", err)
	}
	return nil
}

// isServerRunning checks if a container is already running
func (s *Supervisor) isServerRunning(ctx context.Context, containerName string) bool {
	containers, err := s.containerRuntime.ListRunningContainers(ctx, containerName)
	if err != nil {
		return false
	}
	for _, container := range containers {
		if container.Name == containerName {
			s.mu.Lock()
			s.containerIDs[containerName] = container.ID
			s.mu.Unlock()
			return true
		}
	}
	return false
}

// waitForReady polls the container's healthcheck until it passes, fails, or
// the startup timeout (seconds) runs out.
func (s *Supervisor) waitForReady(ctx context.Context, containerName string, startupTimeout int) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(startupTimeout)*time.Second)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server to become ready: %w", ctx.Err())
		case <-ticker.C:
			health, err := s.containerRuntime.GetContainerHealth(ctx, containerName)
			if err != nil {
				continue
			}
			switch health {
			case containerruntime.HealthStatusHealthy:
				return nil
			case containerruntime.HealthStatusUnhealthy:
				return fmt.Errorf("container became unhealthy during startup")
			}
		}
	}
}

// assignPort assigns a port for the server, finding an available one if needed
func (s *Supervisor) assignPort(server config.MCPServerEntry) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if port, exists := s.assignedPorts[server.Name]; exists {
		return port
	}

	port := s.determinePort(server)
	s.assignedPorts[server.Name] = port
	return port
}

// portMappings returns the host:container port mappings for the container
func (s *Supervisor) portMappings(server config.MCPServerEntry, assignedPort int) []string {
	if server.Port > 0 {
		return []string{fmt.Sprintf("%d:3000", assignedPort)}
	}

	if len(server.Ports) > 0 {
		mappings := make([]string, 0, len(server.Ports))
		for i, portMapping := range server.Ports {
			mappings = append(mappings, s.mapPort(portMapping, i, assignedPort))
		}
		return mappings
	}

	return []string{fmt.Sprintf("%d:8080", assignedPort)}
}

// mapPort maps the first configured port onto the assigned host port
func (s *Supervisor) mapPort(portMapping string, index int, assignedPort int) string {
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
func (s *Supervisor) determinePort(server config.MCPServerEntry) int {
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
func (s *Supervisor) getPath(server config.MCPServerEntry) string {
	if server.Path != "" {
		return server.Path
	}
	return "/mcp"
}
