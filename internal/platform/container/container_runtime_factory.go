package container

import (
	"fmt"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// RuntimeType represents the type of container runtime
type RuntimeType string

const (
	RuntimeTypeDocker RuntimeType = "docker"
	RuntimeTypePodman RuntimeType = "podman"
)

// NewContainerRuntime creates a container runtime based on the configured type
// If runtimeType is empty, returns nil to allow binary mode fallback
func NewContainerRuntime(sessionID string, runtimeType RuntimeType) (ContainerRuntime, error) {
	if runtimeType == "" {
		logger.Info("no container runtime configured, will use binary mode")
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

// NewSharedRuntime creates the runtime for the containers `infer mcp start`
// and `infer agents start` run detached. Unlike NewContainerRuntime, an unset
// type is an error, since there is no binary mode to fall back to.
func NewSharedRuntime(runtimeType RuntimeType) (ContainerRuntime, error) {
	rt, err := NewContainerRuntime(SharedSessionID, runtimeType)
	if err != nil {
		return nil, err
	}
	if rt == nil {
		return nil, fmt.Errorf("no container runtime configured (set container_runtime.type to docker or podman)")
	}
	return rt, nil
}
