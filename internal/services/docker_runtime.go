package services

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// DockerRuntime implements ContainerRuntime interface for Docker
type DockerRuntime struct {
	sessionID      domain.SessionID
	networkName    string
	networkCreated bool
}

// NewDockerRuntime creates a new Docker runtime manager
func NewDockerRuntime(sessionID domain.SessionID) domain.ContainerRuntime {
	return &DockerRuntime{
		sessionID:   sessionID,
		networkName: fmt.Sprintf("%s-%s", InferNetworkPrefix, sessionID),
	}
}

// GetNetworkName returns the session-specific network name
func (dr *DockerRuntime) GetNetworkName() string {
	return dr.networkName
}

// EnsureNetwork creates the Docker network if it doesn't exist
func (dr *DockerRuntime) EnsureNetwork(ctx context.Context) error {
	if dr.networkCreated {
		return nil
	}

	cmd := exec.CommandContext(ctx, "docker", "network", "inspect", dr.networkName)
	if err := cmd.Run(); err == nil {
		dr.networkCreated = true
		return nil
	}

	logger.Info("Creating Docker network", "session", dr.sessionID, "network", dr.networkName)
	cmd = exec.CommandContext(ctx, "docker", "network", "create", dr.networkName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already exists") {
			dr.networkCreated = true
			return nil
		}
		return fmt.Errorf("failed to create Docker network: %w, output: %s", err, string(output))
	}

	dr.networkCreated = true
	logger.Info("Docker network created successfully", "session", dr.sessionID, "network", dr.networkName)
	return nil
}

// CleanupNetwork removes the session-specific network
func (dr *DockerRuntime) CleanupNetwork(ctx context.Context) error {
	if !dr.networkCreated {
		return nil
	}

	cmd := exec.CommandContext(ctx, "docker", "network", "rm", dr.networkName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove Docker network %s: %w", dr.networkName, err)
	}

	dr.networkCreated = false
	logger.Info("Docker network removed successfully", "session", dr.sessionID, "network", dr.networkName)
	return nil
}

// ContainerExists checks if a Docker container exists by ID or name (running or stopped)
func (dr *DockerRuntime) ContainerExists(containerIDOrName string) bool {
	if containerIDOrName == "" {
		return false
	}
	cmd := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", containerIDOrName)
	return cmd.Run() == nil
}

// RunContainer runs a Docker container with the given options
func (dr *DockerRuntime) RunContainer(ctx context.Context, opts domain.RunContainerOptions) (string, error) {
	args := []string{"run"}

	if opts.Detached {
		args = append(args, "-d")
	}

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}

	for _, port := range opts.Ports {
		args = append(args, "-p", port)
	}

	if opts.RemoveOnExit {
		args = append(args, "--rm")
	}

	if opts.EnvFile != "" {
		args = append(args, "--env-file", opts.EnvFile)
	}

	for key, value := range opts.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	for _, volume := range opts.Volumes {
		args = append(args, "-v", volume)
	}

	if opts.HealthCmd != "" {
		healthConfig := opts.HealthConfig
		if healthConfig == nil {
			healthConfig = &domain.HealthCheckConfig{
				Interval:    "10s",
				Timeout:     "5s",
				Retries:     3,
				StartPeriod: "10s",
			}
		}

		args = append(args,
			"--health-cmd", opts.HealthCmd,
			"--health-interval", healthConfig.Interval,
			"--health-timeout", healthConfig.Timeout,
			"--health-retries", fmt.Sprintf("%d", healthConfig.Retries),
			"--health-start-period", healthConfig.StartPeriod,
		)
	}

	if len(opts.Entrypoint) > 0 {
		args = append(args, "--entrypoint", opts.Entrypoint[0])
	}

	args = append(args, opts.Image)

	if len(opts.Entrypoint) > 1 {
		args = append(args, opts.Entrypoint[1:]...)
	} else if len(opts.Command) > 0 {
		args = append(args, opts.Command...)
	}

	if len(opts.Args) > 0 {
		args = append(args, opts.Args...)
	}

	logger.Debug("Running Docker container", "command", fmt.Sprintf("docker %s", strings.Join(args, " ")))

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker run failed: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// StopContainer stops a Docker container
func (dr *DockerRuntime) StopContainer(ctx context.Context, containerIDOrName string) error {
	if !dr.ContainerExists(containerIDOrName) {
		return nil
	}

	cmd := exec.CommandContext(ctx, "docker", "stop", containerIDOrName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", containerIDOrName, err)
	}

	return nil
}

// PullImage pulls a Docker image
func (dr *DockerRuntime) PullImage(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull failed: %w, output: %s", err, string(output))
	}
	return nil
}

// GetContainerHealth returns the health status of a container
func (dr *DockerRuntime) GetContainerHealth(ctx context.Context, containerIDOrName string) (domain.HealthStatus, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Health.Status}}", containerIDOrName)
	output, err := cmd.Output()
	if err != nil {
		return domain.HealthStatusNone, fmt.Errorf("failed to inspect container: %w", err)
	}

	healthStr := strings.TrimSpace(string(output))
	switch healthStr {
	case "healthy":
		return domain.HealthStatusHealthy, nil
	case "unhealthy":
		return domain.HealthStatusUnhealthy, nil
	case "starting":
		return domain.HealthStatusStarting, nil
	default:
		return domain.HealthStatusNone, nil
	}
}

// ListRunningContainers lists all running containers matching the name filter
func (dr *DockerRuntime) ListRunningContainers(ctx context.Context, nameFilter string) ([]domain.ContainerInfo, error) {
	args := []string{"ps", "--format", "{{.ID}}\t{{.Names}}"}
	if nameFilter != "" {
		args = append(args, "--filter", fmt.Sprintf("name=%s", nameFilter))
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w, output: %s", err, string(output))
	}

	var containers []domain.ContainerInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			continue
		}
		containers = append(containers, domain.ContainerInfo{
			ID:   parts[0],
			Name: parts[1],
		})
	}

	return containers, nil
}
