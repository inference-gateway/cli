package services

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

const (
	// AgentContainerPrefix is the naming prefix for agent containers
	AgentContainerPrefix = "inference-agent-"
)

// AgentManager manages the lifecycle of A2A agent containers
type AgentManager struct {
	config       *config.Config
	agentsConfig *config.AgentsConfig
	containers   map[string]string
	isRunning    bool
}

// NewAgentManager creates a new agent manager
func NewAgentManager(cfg *config.Config, agentsConfig *config.AgentsConfig) *AgentManager {
	return &AgentManager{
		config:       cfg,
		agentsConfig: agentsConfig,
		containers:   make(map[string]string),
	}
}

// StartAgents starts all agents configured with run: true
func (am *AgentManager) StartAgents(ctx context.Context) error {
	var startedCount int
	var errors []error

	for _, agent := range am.agentsConfig.Agents {
		if !agent.Run {
			continue
		}

		if err := am.StartAgent(ctx, agent); err != nil {
			logger.Warn("Failed to start agent", "name", agent.Name, "error", err)
			errors = append(errors, fmt.Errorf("agent %s: %w", agent.Name, err))
			continue
		}

		startedCount++
	}

	if len(errors) > 0 && startedCount == 0 {
		return fmt.Errorf("failed to start any agents: %v", errors)
	}

	if startedCount > 0 {
		am.isRunning = true
		logger.Info("Started agents", "count", startedCount)
	}

	return nil
}

// StartAgent starts a single agent container
func (am *AgentManager) StartAgent(ctx context.Context, agent config.AgentEntry) error {
	if agent.OCI == "" {
		return fmt.Errorf("agent %s has run: true but no OCI image specified", agent.Name)
	}

	logger.Info("Starting agent container", "name", agent.Name, "image", agent.OCI)

	if am.isAgentRunning(agent.Name) {
		logger.Info("Agent container is already running", "name", agent.Name)
		return nil
	}

	if err := am.pullImage(ctx, agent.OCI); err != nil {
		logger.Warn("Failed to pull agent image, attempting to use local image", "name", agent.Name, "error", err)
	}

	if err := am.startContainer(ctx, agent); err != nil {
		return fmt.Errorf("failed to start agent container: %w", err)
	}

	if err := am.waitForReady(ctx, agent); err != nil {
		_ = am.StopAgent(ctx, agent.Name)
		return fmt.Errorf("agent failed to become ready: %w", err)
	}

	logger.Info("Agent container started successfully", "name", agent.Name, "url", agent.URL)
	return nil
}

// StopAgents stops all running agent containers
func (am *AgentManager) StopAgents(ctx context.Context) error {
	logger.Info("Stopping agents", "trackedCount", len(am.containers))

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

	logger.Info("Stopping agent container", "name", agentName, "containerID", containerID)

	cmd := exec.CommandContext(ctx, "docker", "stop", containerID)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to stop agent container", "name", agentName, "error", err)
	}

	cmd = exec.CommandContext(ctx, "docker", "rm", containerID)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to remove agent container", "name", agentName, "error", err)
	}

	delete(am.containers, agentName)
	logger.Info("Agent container stopped", "name", agentName)
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
		"-p", fmt.Sprintf("%s:8080", port),
		"--rm",
	}

	env := agent.GetEnvironmentWithModel()
	env["A2A_AGENT_CLIENT_BASE_URL"] = am.config.Gateway.URL

	for key, value := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	args = append(args, agent.OCI)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check for port collision errors
		outputStr := string(output)
		if strings.Contains(outputStr, "port is already allocated") ||
			strings.Contains(outputStr, "address already in use") {
			return &domain.PortCollisionError{
				Port:    port,
				Service: agent.Name,
			}
		}
		return fmt.Errorf("docker run failed: %w, output: %s", err, outputStr)
	}

	containerID := strings.TrimSpace(string(output))
	am.containers[agent.Name] = containerID
	return nil
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
		am.containers[agentName] = containerID
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
