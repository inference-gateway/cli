package services

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	gotenv "github.com/subosito/gotenv"
)

const (
	// AgentContainerPrefix is the naming prefix for agent containers
	AgentContainerPrefix = "inference-agent-"
)

// AgentManager manages the lifecycle of A2A agent containers
type AgentManager struct {
	sessionID       domain.SessionID
	config          *config.Config
	agentsConfig    *config.AgentsConfig
	containers      map[string]string
	assignedPorts   map[string]int
	isRunning       bool
	statusCallback  func(agentName string, state domain.AgentState, message string, url string, image string)
	containersMutex sync.Mutex
}

// NewAgentManager creates a new agent manager
func NewAgentManager(sessionID domain.SessionID, cfg *config.Config, agentsConfig *config.AgentsConfig) *AgentManager {
	return &AgentManager{
		sessionID:     sessionID,
		config:        cfg,
		agentsConfig:  agentsConfig,
		containers:    make(map[string]string),
		assignedPorts: make(map[string]int),
	}
}

// getNetworkName returns the session-specific network name
func (am *AgentManager) getNetworkName() string {
	return fmt.Sprintf("%s-%s", InferNetworkPrefix, am.sessionID)
}

// SetStatusCallback sets the callback function for agent status updates
func (am *AgentManager) SetStatusCallback(callback func(agentName string, state domain.AgentState, message string, url string, image string)) {
	am.statusCallback = callback
}

// notifyStatus calls the status callback if set
func (am *AgentManager) notifyStatus(agentName string, state domain.AgentState, message string, url string, image string) {
	if am.statusCallback != nil {
		am.statusCallback(agentName, state, message, url, image)
	}
}

// StartAgents starts all agents configured with run: true asynchronously
func (am *AgentManager) StartAgents(ctx context.Context) error {
	agentsToStart := []config.AgentEntry{}
	for _, agent := range am.agentsConfig.Agents {
		if agent.Run {
			agentsToStart = append(agentsToStart, agent)
		}
	}

	if len(agentsToStart) == 0 {
		return nil
	}

	for _, agent := range agentsToStart {
		go am.startAgentAsync(ctx, agent)
	}

	am.isRunning = true
	logger.Info("Starting agents in background", "count", len(agentsToStart))

	return nil
}

// startAgentAsync starts a single agent asynchronously with status updates
func (am *AgentManager) startAgentAsync(ctx context.Context, agent config.AgentEntry) {
	if err := am.StartAgent(ctx, agent); err != nil {
		logger.Warn("Failed to start agent", "name", agent.Name, "error", err)
		am.notifyStatus(agent.Name, domain.AgentStateFailed, fmt.Sprintf("Failed to start: %v", err), agent.URL, agent.OCI)
	}
}

// StartAgent starts a single agent container with status updates
func (am *AgentManager) StartAgent(ctx context.Context, agent config.AgentEntry) error {
	if agent.OCI == "" {
		return fmt.Errorf("agent %s has run: true but no OCI image specified", agent.Name)
	}

	logger.Info("Starting agent container", "name", agent.Name, "image", agent.OCI)

	if am.isAgentRunning(agent.Name) {
		logger.Info("Agent container is already running", "name", agent.Name)
		am.notifyStatus(agent.Name, domain.AgentStateReady, "Already running", agent.URL, agent.OCI)
		return nil
	}

	am.notifyStatus(agent.Name, domain.AgentStatePullingImage, fmt.Sprintf("Pulling image: %s", agent.OCI), agent.URL, agent.OCI)
	if err := am.pullImage(ctx, agent.OCI); err != nil {
		logger.Warn("Failed to pull agent image, attempting to use local image", "name", agent.Name, "error", err)
	}

	am.notifyStatus(agent.Name, domain.AgentStateStarting, "Starting container", agent.URL, agent.OCI)
	if err := am.startContainer(ctx, agent); err != nil {
		return fmt.Errorf("failed to start agent container: %w", err)
	}

	am.notifyStatus(agent.Name, domain.AgentStateWaitingReady, "Waiting for health check", agent.URL, agent.OCI)
	if err := am.waitForReady(ctx, agent); err != nil {
		if stopErr := am.StopAgent(ctx, agent.Name); stopErr != nil {
			logger.Warn("Failed to stop agent during error cleanup", "name", agent.Name, "error", stopErr)
		}
		return fmt.Errorf("agent failed to become ready: %w", err)
	}

	am.notifyStatus(agent.Name, domain.AgentStateReady, "Ready", agent.URL, agent.OCI)
	logger.Info("Agent container started successfully", "name", agent.Name, "url", agent.URL)
	return nil
}

// StopAgents stops all running agent containers
func (am *AgentManager) StopAgents(ctx context.Context) error {
	for agentName := range am.containers {
		if err := am.StopAgent(ctx, agentName); err != nil {
			logger.Warn("Failed to stop agent", "name", agentName, "error", err)
		}
	}

	am.isRunning = false
	return nil
}

// IsRunning returns whether any agents are running
func (am *AgentManager) IsRunning() bool {
	return am.isRunning
}

// StopAgent stops a single agent container
func (am *AgentManager) StopAgent(ctx context.Context, agentName string) error {
	containerID, exists := am.containers[agentName]
	if !exists || containerID == "" {
		return nil
	}

	if !am.containerExists(containerID) {
		delete(am.containers, agentName)
		return nil
	}

	cmd := exec.CommandContext(ctx, "docker", "stop", containerID)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to stop agent container", "name", agentName, "error", err)
	}

	delete(am.containers, agentName)
	return nil
}

// pullImage pulls the OCI image for an agent
func (am *AgentManager) pullImage(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull failed: %w, output: %s", err, string(output))
	}
	return nil
}

// startContainer starts the agent container
func (am *AgentManager) startContainer(ctx context.Context, agent config.AgentEntry) error {
	assignedPort := am.assignPort(agent)
	containerPort := "8080"

	containerName := fmt.Sprintf("%s%s-%s", AgentContainerPrefix, agent.Name, am.sessionID)
	networkName := am.getNetworkName()
	args := []string{
		"run",
		"-d",
		"--name", containerName,
		"--network", networkName,
		"-p", fmt.Sprintf("%d:%s", assignedPort, containerPort),
		"--rm",
	}

	if agent.ArtifactsURL != "" {
		artifactsBasePort := am.extractPortFromURL(agent.ArtifactsURL)
		if artifactsBasePort <= 0 {
			artifactsBasePort = 8081
		}
		artifactsPort := config.FindAvailablePort(artifactsBasePort)
		args = append(args, "-p", fmt.Sprintf("%d:8081", artifactsPort))
		logger.Info("Assigned artifacts port", "session", am.sessionID, "agent", agent.Name, "port", artifactsPort)
	}

	dotEnvVars, err := am.loadDotEnvFile()
	if err != nil {
		logger.Warn("Could not load .env file", "error", err)
	}

	env := agent.GetEnvironmentWithModel()

	var gatewayURL string
	if am.config.Gateway.Docker {
		gatewayURL = fmt.Sprintf("http://inference-gateway-%s:8080/v1", am.sessionID)
	} else {
		gatewayURL = am.config.Gateway.URL
		if !strings.HasSuffix(gatewayURL, "/v1") {
			gatewayURL = strings.TrimSuffix(gatewayURL, "/") + "/v1"
		}
	}
	env["A2A_AGENT_CLIENT_BASE_URL"] = gatewayURL

	// Configure artifacts server if artifacts_url is specified
	if agent.ArtifactsURL != "" {
		env["A2A_ARTIFACTS_ENABLE"] = "true"
		env["A2A_ARTIFACTS_SERVER_HOST"] = "0.0.0.0"
		env["A2A_ARTIFACTS_SERVER_PORT"] = "8081"
		env["A2A_ARTIFACTS_STORAGE_BASE_URL"] = agent.ArtifactsURL
	}

	resolvedEnv := make(map[string]string)
	for key := range env {
		if value, exists := dotEnvVars[key]; exists {
			resolvedEnv[key] = value
			logger.Warn("Using .env value for variable", "key", key)
		} else if value, exists := os.LookupEnv(key); exists {
			resolvedEnv[key] = value
			logger.Warn("Using system environment value for variable", "key", key)
		} else {
			resolvedEnv[key] = env[key]
		}
	}

	for key, value := range resolvedEnv {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	args = append(args, agent.OCI)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run failed: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))
	am.containersMutex.Lock()
	am.containers[agent.Name] = containerID
	am.containersMutex.Unlock()
	return nil
}

// loadDotEnvFile loads environment variables from .env file in the current directory
func (am *AgentManager) loadDotEnvFile() (map[string]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %w", err)
	}

	dotEnvPath := filepath.Join(cwd, ".env")
	if _, err := os.Stat(dotEnvPath); os.IsNotExist(err) {
		return nil, fmt.Errorf(".env file not found at %s", dotEnvPath)
	}

	envMap, err := gotenv.Read(dotEnvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read .env file: %w", err)
	}

	logger.Info("Loaded .env file", "path", dotEnvPath, "vars", len(envMap))
	return envMap, nil
}

// isAgentRunning checks if an agent container is already running
func (am *AgentManager) isAgentRunning(agentName string) bool {
	expectedName := fmt.Sprintf("%s%s-%s", AgentContainerPrefix, agentName, am.sessionID)
	cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("name=%s", AgentContainerPrefix), "--format", "{{.ID}}\t{{.Names}}")
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

		if foundName == expectedName {
			am.containersMutex.Lock()
			am.containers[agentName] = containerID
			am.containersMutex.Unlock()
			return true
		}
	}
	return false
}

// waitForReady waits for an agent to become ready
func (am *AgentManager) waitForReady(ctx context.Context, agent config.AgentEntry) error {
	healthURL := strings.TrimSuffix(agent.URL, "/") + "/health"

	timeout := 30 * time.Second
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for agent to become ready")
			}

			resp, err := client.Get(healthURL)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

// containerExists checks if a Docker container exists by ID (running or stopped)
func (am *AgentManager) containerExists(containerID string) bool {
	if containerID == "" {
		return false
	}
	cmd := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", containerID)
	return cmd.Run() == nil
}

// assignPort assigns a port for the agent, finding an available one if needed
func (am *AgentManager) assignPort(agent config.AgentEntry) int {
	am.containersMutex.Lock()
	defer am.containersMutex.Unlock()

	if port, exists := am.assignedPorts[agent.Name]; exists {
		return port
	}

	port := am.determineAgentPort(agent)
	am.assignedPorts[agent.Name] = port
	logger.Info("Assigned agent port", "session", am.sessionID, "agent", agent.Name, "port", port)
	return port
}

// determineAgentPort determines the port to use for an agent
func (am *AgentManager) determineAgentPort(agent config.AgentEntry) int {
	basePort := am.extractPortFromURL(agent.URL)
	if basePort <= 0 {
		basePort = 8080
	}

	return config.FindAvailablePort(basePort)
}

// extractPortFromURL extracts the port number from an agent URL
func (am *AgentManager) extractPortFromURL(url string) int {
	if !strings.Contains(url, ":") {
		return 8080
	}

	parts := strings.Split(url, ":")
	if len(parts) == 0 {
		return 8080
	}

	portStr := strings.TrimPrefix(parts[len(parts)-1], "/")
	portStr = strings.Split(portStr, "/")[0]

	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return 8080
	}

	return port
}
