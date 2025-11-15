package domain

import (
	"errors"
	"time"
)

// Common errors
var (
	ErrContainerNotFound = errors.New("container not found")
)

// AgentConfigFile represents the structure of the agents.yaml file
type AgentConfigFile struct {
	Agents []AgentDefinition `yaml:"agents" mapstructure:"agents"`
}

// AgentDefinition represents a single A2A agent configuration
type AgentDefinition struct {
	// Name is the friendly name of the agent
	Name string `yaml:"name" mapstructure:"name"`

	// URL is the endpoint where the agent is accessible
	URL string `yaml:"url" mapstructure:"url"`

	// OCI is the container image for local execution (optional)
	OCI string `yaml:"oci,omitempty" mapstructure:"oci,omitempty"`

	// Run indicates whether to run the agent locally with Docker
	Run bool `yaml:"run" mapstructure:"run"`

	// Environment variables for Docker execution
	Environment map[string]string `yaml:"environment,omitempty" mapstructure:"environment,omitempty"`

	// Description provides a brief description of the agent's capabilities
	Description string `yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// Enabled indicates whether the agent is currently active
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Metadata for additional agent information
	Metadata map[string]string `yaml:"metadata,omitempty" mapstructure:"metadata,omitempty"`

	// Docker execution configuration
	Docker *AgentDockerConfig `yaml:"docker,omitempty" mapstructure:"docker,omitempty"`
}

// AgentDockerConfig contains Docker-specific configuration for local agent execution
type AgentDockerConfig struct {
	// Image is the Docker image to use (defaults to OCI if not specified)
	Image string `yaml:"image,omitempty" mapstructure:"image,omitempty"`

	// Port is the port to expose the agent on
	Port int `yaml:"port,omitempty" mapstructure:"port,omitempty"`

	// HostPort is the host port to bind to (optional, defaults to Port)
	HostPort int `yaml:"host_port,omitempty" mapstructure:"host_port,omitempty"`

	// Volumes to mount in the container
	Volumes []string `yaml:"volumes,omitempty" mapstructure:"volumes,omitempty"`

	// NetworkMode for the Docker container
	NetworkMode string `yaml:"network_mode,omitempty" mapstructure:"network_mode,omitempty"`

	// RestartPolicy for the container
	RestartPolicy string `yaml:"restart_policy,omitempty" mapstructure:"restart_policy,omitempty"`

	// HealthCheck configuration
	HealthCheck *DockerHealthCheck `yaml:"health_check,omitempty" mapstructure:"health_check,omitempty"`
}

// DockerHealthCheck defines health check configuration for Docker containers
type DockerHealthCheck struct {
	// Test command to run for health check
	Test []string `yaml:"test,omitempty" mapstructure:"test,omitempty"`

	// Interval between health checks
	Interval time.Duration `yaml:"interval,omitempty" mapstructure:"interval,omitempty"`

	// Timeout for each health check
	Timeout time.Duration `yaml:"timeout,omitempty" mapstructure:"timeout,omitempty"`

	// Retries before marking as unhealthy
	Retries int `yaml:"retries,omitempty" mapstructure:"retries,omitempty"`

	// StartPeriod before health checks start
	StartPeriod time.Duration `yaml:"start_period,omitempty" mapstructure:"start_period,omitempty"`
}

// AgentConfigService manages agent configurations
type AgentConfigService interface {
	// LoadAgents loads agents from the configuration file
	LoadAgents() (*AgentConfigFile, error)

	// SaveAgents saves agents to the configuration file
	SaveAgents(config *AgentConfigFile) error

	// AddAgent adds a new agent to the configuration
	AddAgent(agent AgentDefinition) error

	// RemoveAgent removes an agent from the configuration
	RemoveAgent(name string) error

	// GetAgent retrieves an agent by name
	GetAgent(name string) (*AgentDefinition, error)

	// ListAgents returns all configured agents
	ListAgents() ([]AgentDefinition, error)

	// GetConfigPath returns the path to the agents configuration file
	GetConfigPath() (string, error)

	// StartAgent starts a Docker-based agent locally
	StartAgent(name string) error

	// StopAgent stops a running Docker-based agent
	StopAgent(name string) error

	// GetAgentStatus returns the status of a Docker-based agent
	GetAgentStatus(name string) (AgentStatus, error)
}

// AgentStatus represents the current status of an agent
type AgentStatus struct {
	Name      string            `json:"name"`
	Running   bool              `json:"running"`
	URL       string            `json:"url"`
	Container string            `json:"container,omitempty"`
	Health    string            `json:"health,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// DockerService handles Docker operations for agents
type DockerService interface {
	// StartContainer starts a Docker container for an agent
	StartContainer(agent AgentDefinition) (string, error)

	// StopContainer stops a Docker container
	StopContainer(containerID string) error

	// GetContainerStatus returns the status of a container
	GetContainerStatus(containerID string) (*ContainerStatus, error)

	// IsDockerAvailable checks if Docker is available
	IsDockerAvailable() bool
}

// ContainerStatus represents the status of a Docker container
type ContainerStatus struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Health  string `json:"health,omitempty"`
	Ports   string `json:"ports,omitempty"`
	Created string `json:"created"`
}
