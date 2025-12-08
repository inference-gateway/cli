package services

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// PodmanRuntime implements ContainerRuntime interface for Podman
type PodmanRuntime struct {
	sessionID      domain.SessionID
	networkName    string
	networkCreated bool
}

// NewPodmanRuntime creates a new Podman runtime manager
func NewPodmanRuntime(sessionID domain.SessionID) domain.ContainerRuntime {
	return &PodmanRuntime{
		sessionID:   sessionID,
		networkName: fmt.Sprintf("%s-%s", InferNetworkPrefix, sessionID),
	}
}

// GetNetworkName returns the session-specific network name
func (pr *PodmanRuntime) GetNetworkName() string {
	return pr.networkName
}

// EnsureNetwork creates the Podman network if it doesn't exist
func (pr *PodmanRuntime) EnsureNetwork(ctx context.Context) error {
	if pr.networkCreated {
		return nil
	}

	cmd := exec.CommandContext(ctx, "podman", "network", "inspect", pr.networkName)
	if err := cmd.Run(); err == nil {
		pr.networkCreated = true
		return nil
	}

	logger.Info("Creating Podman network", "session", pr.sessionID, "network", pr.networkName)
	cmd = exec.CommandContext(ctx, "podman", "network", "create", pr.networkName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already exists") {
			pr.networkCreated = true
			return nil
		}
		return fmt.Errorf("failed to create Podman network: %w, output: %s", err, string(output))
	}

	pr.networkCreated = true
	logger.Info("Podman network created successfully", "session", pr.sessionID, "network", pr.networkName)
	return nil
}

// CleanupNetwork removes the session-specific network
func (pr *PodmanRuntime) CleanupNetwork(ctx context.Context) error {
	if !pr.networkCreated {
		return nil
	}

	cmd := exec.CommandContext(ctx, "podman", "network", "rm", pr.networkName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove Podman network %s: %w", pr.networkName, err)
	}

	pr.networkCreated = false
	logger.Info("Podman network removed successfully", "session", pr.sessionID, "network", pr.networkName)
	return nil
}

// ContainerExists checks if a Podman container exists by ID or name (running or stopped)
func (pr *PodmanRuntime) ContainerExists(containerIDOrName string) bool {
	if containerIDOrName == "" {
		return false
	}
	cmd := exec.Command("podman", "inspect", "--format", "{{.State.Status}}", containerIDOrName)
	return cmd.Run() == nil
}

// RunContainer runs a Podman container with the given options
func (pr *PodmanRuntime) RunContainer(ctx context.Context, opts domain.RunContainerOptions) (string, error) {
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

	logger.Debug("Running Podman container", "command", fmt.Sprintf("podman %s", strings.Join(args, " ")))

	cmd := exec.CommandContext(ctx, "podman", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("podman run failed: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// StopContainer stops a Podman container
func (pr *PodmanRuntime) StopContainer(ctx context.Context, containerIDOrName string) error {
	if !pr.ContainerExists(containerIDOrName) {
		return nil
	}

	cmd := exec.CommandContext(ctx, "podman", "stop", containerIDOrName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", containerIDOrName, err)
	}

	return nil
}

// PullImage pulls a Podman image
func (pr *PodmanRuntime) PullImage(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "podman", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman pull failed: %w, output: %s", err, string(output))
	}
	return nil
}

// GetContainerHealth returns the health status of a container
func (pr *PodmanRuntime) GetContainerHealth(ctx context.Context, containerIDOrName string) (domain.HealthStatus, error) {
	cmd := exec.CommandContext(ctx, "podman", "inspect", "--format", "{{.State.Health.Status}}", containerIDOrName)
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
func (pr *PodmanRuntime) ListRunningContainers(ctx context.Context, nameFilter string) ([]domain.ContainerInfo, error) {
	args := []string{"ps", "--format", "{{.ID}}\t{{.Names}}"}
	if nameFilter != "" {
		args = append(args, "--filter", fmt.Sprintf("name=%s", nameFilter))
	}

	cmd := exec.CommandContext(ctx, "podman", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("podman ps failed: %w, output: %s", err, string(output))
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
