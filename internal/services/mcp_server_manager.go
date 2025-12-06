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
)

// Compile-time check that MCPServerManager implements domain.MCPServerManager
var _ domain.MCPServerManager = (*MCPServerManager)(nil)

// MCPServerManager manages the lifecycle of MCP server containers
type MCPServerManager struct {
	config        *config.MCPConfig
	containerIDs  map[string]string
	assignedPorts map[string]int // serverName -> assigned port
	mu            sync.Mutex
}

// NewMCPServerManager creates a new MCP server manager
func NewMCPServerManager(cfg *config.MCPConfig) *MCPServerManager {
	return &MCPServerManager{
		config:        cfg,
		containerIDs:  make(map[string]string),
		assignedPorts: make(map[string]int),
	}
}

// StartServers starts all MCP servers that have run=true
// This method is non-fatal and always returns nil
func (m *MCPServerManager) StartServers(ctx context.Context) error {
	serversToStart := make([]config.MCPServerEntry, 0)
	for _, server := range m.config.Servers {
		if server.Run && server.Enabled {
			serversToStart = append(serversToStart, server)
		}
	}

	if len(serversToStart) == 0 {
		return nil
	}

	if err := m.ensureNetwork(ctx); err != nil {
		logger.Warn("Failed to create Docker network", "error", err)
	}

	var wg sync.WaitGroup
	for _, server := range serversToStart {
		wg.Add(1)
		go func(srv config.MCPServerEntry) {
			defer wg.Done()
			if err := m.StartServer(ctx, srv); err != nil {
				logger.Warn("Failed to start MCP server",
					"server", srv.Name,
					"error", err,
					"container", fmt.Sprintf("inference-mcp-%s", srv.Name))
			}
		}(server)
	}

	wg.Wait()
	return nil
}

// StartServer starts a single MCP server container
func (m *MCPServerManager) StartServer(ctx context.Context, server config.MCPServerEntry) error {
	containerName := fmt.Sprintf("inference-mcp-%s", server.Name)

	assignedPort := m.assignPort(server)

	if m.isServerRunning(containerName) {
		logger.Info("MCP server container already running", "server", server.Name, "port", assignedPort)
		return nil
	}

	if err := m.pullImage(ctx, server.OCI); err != nil {
		logger.Warn("Failed to pull image, using cached version", "image", server.OCI, "error", err)
	}

	logger.Info("Starting MCP server", "server", server.Name, "port", assignedPort)

	if err := m.startContainer(ctx, server, assignedPort); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	logger.Info("Waiting for MCP server to become ready", "server", server.Name)

	if err := m.waitForReady(ctx, server, assignedPort); err != nil {
		_ = m.stopContainer(ctx, containerName)
		return fmt.Errorf("server failed to become ready: %w", err)
	}

	fullURL := fmt.Sprintf("http://localhost:%d%s", assignedPort, m.getPath(server))
	logger.Info("MCP server started successfully", "server", server.Name, "url", fullURL)
	return nil
}

// StopServers stops all running MCP server containers
func (m *MCPServerManager) StopServers(ctx context.Context) error {
	m.mu.Lock()
	containerNames := make([]string, 0, len(m.containerIDs))
	for k := range m.containerIDs {
		containerNames = append(containerNames, k)
	}
	m.mu.Unlock()

	for _, name := range containerNames {
		if err := m.stopContainer(ctx, name); err != nil {
			logger.Warn("Failed to stop MCP server container", "server", name, "error", err)
		} else {
			logger.Info("Stopped MCP server container", "server", name)
		}
		m.mu.Lock()
		delete(m.containerIDs, name)
		m.mu.Unlock()
	}

	return nil
}

// pullImage pulls the container image
func (m *MCPServerManager) pullImage(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull failed: %w, output: %s", err, string(output))
	}
	return nil
}

// startContainer starts the MCP server container
func (m *MCPServerManager) startContainer(ctx context.Context, server config.MCPServerEntry, assignedPort int) error {
	containerName := fmt.Sprintf("inference-mcp-%s", server.Name)

	args := []string{
		"run",
		"-d",
		"--name", containerName,
		"--network", InferNetworkName,
		"--restart", "unless-stopped",
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
func (m *MCPServerManager) stopContainer(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", containerName)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to stop container", "container", containerName, "error", err)
	}

	cmd = exec.CommandContext(ctx, "docker", "rm", containerName)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to remove container", "container", containerName, "error", err)
	}

	return nil
}

// isServerRunning checks if a container is already running
func (m *MCPServerManager) isServerRunning(containerName string) bool {
	cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("name=%s", containerName), "--format", "{{.ID}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	containerID := strings.TrimSpace(string(output))
	if containerID != "" {
		m.mu.Lock()
		m.containerIDs[containerName] = containerID
		m.mu.Unlock()
		return true
	}
	return false
}

// waitForReady waits for the server to become ready by using Docker's healthcheck status
func (m *MCPServerManager) waitForReady(ctx context.Context, server config.MCPServerEntry, assignedPort int) error {
	containerName := fmt.Sprintf("inference-mcp-%s", server.Name)
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

// ensureNetwork creates the Docker network if it doesn't exist
func (m *MCPServerManager) ensureNetwork(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "inspect", InferNetworkName)
	if err := cmd.Run(); err == nil {
		return nil
	}

	logger.Info("Creating Docker network", "network", InferNetworkName)
	cmd = exec.CommandContext(ctx, "docker", "network", "create", InferNetworkName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already exists") {
			return nil
		}
		return fmt.Errorf("failed to create Docker network: %w, output: %s", err, string(output))
	}

	logger.Info("Docker network created successfully", "network", InferNetworkName)
	return nil
}

// assignPort assigns a port for the server, finding an available one if needed
func (m *MCPServerManager) assignPort(server config.MCPServerEntry) int {
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
func (m *MCPServerManager) appendPortMappings(args []string, server config.MCPServerEntry, assignedPort int) []string {
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
func (m *MCPServerManager) mapPort(portMapping string, index int, assignedPort int) string {
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
func (m *MCPServerManager) determinePort(server config.MCPServerEntry) int {
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
func (m *MCPServerManager) getPath(server config.MCPServerEntry) string {
	if server.Path != "" {
		return server.Path
	}
	return "/mcp"
}
