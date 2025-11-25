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
	config          *config.Config
	agentsConfig    *config.AgentsConfig
	containers      map[string]string
	isRunning       bool
	statusCallback  func(agentName string, state domain.AgentState, message string, url string, image string)
	containersMutex sync.Mutex
}

// NewAgentManager creates a new agent manager
func NewAgentManager(cfg *config.Config, agentsConfig *config.AgentsConfig) *AgentManager {
	return &AgentManager{
		config:       cfg,
		agentsConfig: agentsConfig,
		containers:   make(map[string]string),
	}
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
		_ = am.StopAgent(ctx, agent.Name)
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

	cmd := exec.CommandContext(ctx, "docker", "stop", containerID)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to stop agent container", "name", agentName, "error", err)
	}

	cmd = exec.CommandContext(ctx, "docker", "rm", containerID)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to remove agent container", "name", agentName, "error", err)
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
	port := "8080"
	if strings.Contains(agent.URL, ":") {
		parts := strings.Split(agent.URL, ":")
		if len(parts) > 0 {
			port = strings.TrimPrefix(parts[len(parts)-1], "/")
		}
	}

	args := []string{
		"run",
		"-d",
		"--name", fmt.Sprintf("%s%s", AgentContainerPrefix, agent.Name),
		"--network", InferNetworkName,
		"-p", fmt.Sprintf("%s:8080", port),
		"--rm",
	}

	if agent.ArtifactsURL != "" {
		artifactsPort := "8081"
		if strings.Contains(agent.ArtifactsURL, ":") {
			parts := strings.Split(agent.ArtifactsURL, ":")
			if len(parts) > 0 {
				artifactsPort = strings.TrimPrefix(parts[len(parts)-1], "/")
			}
		}
		args = append(args, "-p", fmt.Sprintf("%s:8081", artifactsPort))
	}

	dotEnvVars, err := am.loadDotEnvFile()
	if err != nil {
		logger.Warn("Could not load .env file", "error", err)
	}

	env := agent.GetEnvironmentWithModel()

	gatewayURL := "http://inference-gateway:8080/v1"
	if !am.config.Gateway.Docker {
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
	containerName := fmt.Sprintf("%s%s", AgentContainerPrefix, agentName)
	cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("name=%s", containerName), "--format", "{{.ID}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	containerID := strings.TrimSpace(string(output))
	if containerID != "" {
		am.containersMutex.Lock()
		am.containers[agentName] = containerID
		am.containersMutex.Unlock()
		return true
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
