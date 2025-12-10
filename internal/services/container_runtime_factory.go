package services

import (
	"fmt"

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
// If runtimeType is empty, returns nil to allow binary mode fallback
func NewContainerRuntime(sessionID domain.SessionID, runtimeType RuntimeType) (domain.ContainerRuntime, error) {
	if runtimeType == "" {
		logger.Info("No container runtime configured, will use binary mode")
		return nil, nil
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
