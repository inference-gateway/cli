package services

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

type DockerServiceImpl struct {
	ctx context.Context
}

func NewDockerService(ctx context.Context) domain.DockerService {
	return &DockerServiceImpl{
		ctx: ctx,
	}
}

func (s *DockerServiceImpl) StartContainer(agent domain.AgentDefinition) (string, error) {
	if !s.IsDockerAvailable() {
		return "", fmt.Errorf("Docker is not available")
	}

	// Determine the image to use
	image := agent.OCI
	if agent.Docker != nil && agent.Docker.Image != "" {
		image = agent.Docker.Image
	}

	if image == "" {
		return "", fmt.Errorf("no Docker image specified for agent %s", agent.Name)
	}

	// Build docker run command
	args := []string{"run", "-d"}

	// Set container name
	containerName := fmt.Sprintf("infer-agent-%s", agent.Name)
	args = append(args, "--name", containerName)

	// Add environment variables
	for key, value := range agent.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	// Handle port configuration
	if agent.Docker != nil {
		port := agent.Docker.Port
		hostPort := agent.Docker.HostPort

		if port > 0 {
			if hostPort == 0 {
				hostPort = port
			}
			args = append(args, "-p", fmt.Sprintf("%d:%d", hostPort, port))
		}

		// Add volumes
		for _, volume := range agent.Docker.Volumes {
			args = append(args, "-v", volume)
		}

		// Set network mode
		if agent.Docker.NetworkMode != "" {
			args = append(args, "--network", agent.Docker.NetworkMode)
		}

		// Set restart policy
		if agent.Docker.RestartPolicy != "" {
			args = append(args, "--restart", agent.Docker.RestartPolicy)
		}

		// Add health check
		if agent.Docker.HealthCheck != nil {
			hc := agent.Docker.HealthCheck
			if len(hc.Test) > 0 {
				args = append(args, "--health-cmd", strings.Join(hc.Test, " "))
			}
			if hc.Interval > 0 {
				args = append(args, "--health-interval", hc.Interval.String())
			}
			if hc.Timeout > 0 {
				args = append(args, "--health-timeout", hc.Timeout.String())
			}
			if hc.Retries > 0 {
				args = append(args, "--health-retries", strconv.Itoa(hc.Retries))
			}
			if hc.StartPeriod > 0 {
				args = append(args, "--health-start-period", hc.StartPeriod.String())
			}
		}
	}

	// Add the image as the last argument
	args = append(args, image)

	logger.Debug("Starting Docker container", "command", strings.Join(append([]string{"docker"}, args...), " "))

	cmd := exec.CommandContext(s.ctx, "docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to start Docker container: %w", err)
	}

	containerID := strings.TrimSpace(string(output))
	logger.Info("Started Docker container", "agent", agent.Name, "container_id", containerID)

	return containerID, nil
}

func (s *DockerServiceImpl) StopContainer(containerID string) error {
	if !s.IsDockerAvailable() {
		return fmt.Errorf("Docker is not available")
	}

	cmd := exec.CommandContext(s.ctx, "docker", "stop", containerID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", containerID, err)
	}

	// Remove the container
	removeCmd := exec.CommandContext(s.ctx, "docker", "rm", containerID)
	if err := removeCmd.Run(); err != nil {
		logger.Warn("Failed to remove stopped container", "container_id", containerID, "error", err)
	}

	logger.Info("Stopped and removed Docker container", "container_id", containerID)
	return nil
}

func (s *DockerServiceImpl) GetContainerStatus(containerID string) (*domain.ContainerStatus, error) {
	if !s.IsDockerAvailable() {
		return nil, fmt.Errorf("Docker is not available")
	}

	// Get container information
	cmd := exec.CommandContext(s.ctx, "docker", "inspect", containerID, "--format",
		"{{.Id}}|{{.Name}}|{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}|{{range .NetworkSettings.Ports}}{{.}}{{end}}|{{.Created}}")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) < 6 {
		return nil, fmt.Errorf("unexpected docker inspect output format")
	}

	status := &domain.ContainerStatus{
		ID:      parts[0],
		Name:    strings.TrimPrefix(parts[1], "/"),
		State:   parts[2],
		Health:  parts[3],
		Ports:   parts[4],
		Created: parts[5],
	}

	return status, nil
}

func (s *DockerServiceImpl) IsDockerAvailable() bool {
	cmd := exec.CommandContext(s.ctx, "docker", "--version")
	if err := cmd.Run(); err != nil {
		return false
	}

	// Check if Docker daemon is running
	cmd = exec.CommandContext(s.ctx, "docker", "info")
	return cmd.Run() == nil
}

// Helper function to check if Docker is available (used by other services)
func IsDockerAvailable(ctx context.Context) bool {
	dockerService := NewDockerService(ctx)
	return dockerService.IsDockerAvailable()
}
