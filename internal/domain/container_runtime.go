package domain

import "context"

// ContainerRuntime defines the interface for container runtime operations
// This abstraction allows support for Docker, Podman, or any other container runtime
type ContainerRuntime interface {
	// Network operations
	GetNetworkName() string
	EnsureNetwork(ctx context.Context) error
	CleanupNetwork(ctx context.Context) error

	// Container lifecycle operations
	ContainerExists(containerIDOrName string) bool
	RunContainer(ctx context.Context, opts RunContainerOptions) (containerID string, err error)
	StopContainer(ctx context.Context, containerIDOrName string) error

	// Image operations
	PullImage(ctx context.Context, image string) error

	// Container inspection
	GetContainerHealth(ctx context.Context, containerIDOrName string) (HealthStatus, error)
	ListRunningContainers(ctx context.Context, nameFilter string) ([]ContainerInfo, error)
}

// RunContainerOptions contains all options for running a container
type RunContainerOptions struct {
	Name         string
	Image        string
	Network      string
	Ports        []string // Format: "host:container" or "host:container/protocol"
	Environment  map[string]string
	Volumes      []string // Format: "host:container" or "host:container:mode"
	Entrypoint   []string
	Command      []string
	Args         []string
	HealthCmd    string
	HealthConfig *HealthCheckConfig
	RemoveOnExit bool
	Detached     bool
	EnvFile      string // Optional .env file path
}

// HealthCheckConfig defines container health check configuration
type HealthCheckConfig struct {
	Interval    string // e.g., "10s"
	Timeout     string // e.g., "5s"
	Retries     int
	StartPeriod string // e.g., "10s"
}

// ContainerInfo represents basic container information
type ContainerInfo struct {
	ID   string
	Name string
}

// HealthStatus represents the health status of a container
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusStarting  HealthStatus = "starting"
	HealthStatusNone      HealthStatus = "none"
)
