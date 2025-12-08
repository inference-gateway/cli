package services

import (
	"fmt"
	"os/exec"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// RuntimeType represents the type of container runtime
type RuntimeType string

const (
	RuntimeTypeDocker RuntimeType = "docker"
	RuntimeTypePodman RuntimeType = "podman"
)

// NewContainerRuntime creates a container runtime based on the configured type
// If runtimeType is empty, it auto-detects the available runtime
func NewContainerRuntime(sessionID domain.SessionID, runtimeType RuntimeType) (domain.ContainerRuntime, error) {
	if runtimeType == "" {
		detected, err := DetectContainerRuntime()
		if err != nil {
			return nil, fmt.Errorf("failed to detect container runtime: %w", err)
		}
		runtimeType = detected
		logger.Info("Auto-detected container runtime", "runtime", runtimeType)
	}

	switch runtimeType {
	case RuntimeTypeDocker:
		return NewDockerRuntime(sessionID), nil
	case RuntimeTypePodman:
		return NewPodmanRuntime(sessionID), nil
	default:
		return nil, fmt.Errorf("unsupported container runtime: %s", runtimeType)
	}
}

// DetectContainerRuntime auto-detects which container runtime is available
func DetectContainerRuntime() (RuntimeType, error) {
	if isCommandAvailable("docker") {
		return RuntimeTypeDocker, nil
	}

	if isCommandAvailable("podman") {
		return RuntimeTypePodman, nil
	}

	// TODO - add support for nerdctl, containerd etc..

	return "", fmt.Errorf("no supported container runtime found (tried: docker, podman)")
}

// isCommandAvailable checks if a command is available in PATH
func isCommandAvailable(command string) bool {
	cmd := exec.Command(command, "version")
	return cmd.Run() == nil
}
