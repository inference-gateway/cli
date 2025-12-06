package config

import (
	"fmt"
	"net"
	"strings"
)

// MCPConfig represents the mcp.yaml configuration file
type MCPConfig struct {
	Enabled               bool             `yaml:"enabled" mapstructure:"enabled"`
	ConnectionTimeout     int              `yaml:"connection_timeout,omitempty" mapstructure:"connection_timeout,omitempty"`
	DiscoveryTimeout      int              `yaml:"discovery_timeout,omitempty" mapstructure:"discovery_timeout,omitempty"`
	LivenessProbeEnabled  bool             `yaml:"liveness_probe_enabled,omitempty" mapstructure:"liveness_probe_enabled,omitempty"`
	LivenessProbeInterval int              `yaml:"liveness_probe_interval,omitempty" mapstructure:"liveness_probe_interval,omitempty"`
	Servers               []MCPServerEntry `yaml:"servers" mapstructure:"servers"`
}

// MCPServerEntry represents a single MCP server configuration
type MCPServerEntry struct {
	Name           string            `yaml:"name" mapstructure:"name"`
	Enabled        bool              `yaml:"enabled" mapstructure:"enabled"`
	Timeout        int               `yaml:"timeout,omitempty" mapstructure:"timeout,omitempty"`
	Description    string            `yaml:"description,omitempty" mapstructure:"description,omitempty"`
	IncludeTools   []string          `yaml:"include_tools,omitempty" mapstructure:"include_tools,omitempty"`
	ExcludeTools   []string          `yaml:"exclude_tools,omitempty" mapstructure:"exclude_tools,omitempty"`
	Run            bool              `yaml:"run" mapstructure:"run"`
	Host           string            `yaml:"host,omitempty" mapstructure:"host,omitempty"`
	Scheme         string            `yaml:"scheme,omitempty" mapstructure:"scheme,omitempty"`
	Port           int               `yaml:"port,omitempty" mapstructure:"port,omitempty"`
	Ports          []string          `yaml:"ports,omitempty" mapstructure:"ports,omitempty"`
	Path           string            `yaml:"path,omitempty" mapstructure:"path,omitempty"`
	OCI            string            `yaml:"oci,omitempty" mapstructure:"oci,omitempty"`
	Entrypoint     []string          `yaml:"entrypoint,omitempty" mapstructure:"entrypoint,omitempty"`
	Command        []string          `yaml:"command,omitempty" mapstructure:"command,omitempty"`
	Args           []string          `yaml:"args,omitempty" mapstructure:"args,omitempty"`
	Env            map[string]string `yaml:"env,omitempty" mapstructure:"env,omitempty"`
	Volumes        []string          `yaml:"volumes,omitempty" mapstructure:"volumes,omitempty"`
	StartupTimeout int               `yaml:"startup_timeout,omitempty" mapstructure:"startup_timeout,omitempty"`
	HealthCmd      string            `yaml:"health_cmd,omitempty" mapstructure:"health_cmd,omitempty"`
}

// ShouldIncludeTool determines if a tool should be included based on include/exclude lists
func (e *MCPServerEntry) ShouldIncludeTool(toolName string) bool {
	if len(e.IncludeTools) > 0 {
		for _, included := range e.IncludeTools {
			if included == toolName {
				return true
			}
		}
		return false
	}

	if len(e.ExcludeTools) > 0 {
		for _, excluded := range e.ExcludeTools {
			if excluded == toolName {
				return false
			}
		}
	}

	return true
}

// GetTimeout returns the effective timeout for this server
func (e *MCPServerEntry) GetTimeout(globalTimeout int) int {
	if e.Timeout > 0 {
		return e.Timeout
	}
	if globalTimeout > 0 {
		return globalTimeout
	}
	return 30
}

// GetStartupTimeout returns the effective startup timeout for this server
func (e *MCPServerEntry) GetStartupTimeout() int {
	if e.StartupTimeout > 0 {
		return e.StartupTimeout
	}
	return 30
}

// GetURL returns the full URL for the server
// Builds from host, scheme, ports, and path
func (e *MCPServerEntry) GetURL() string {
	scheme := e.Scheme
	if scheme == "" {
		scheme = "http"
	}

	host := e.Host
	if host == "" {
		host = "localhost"
	}

	path := e.Path
	if path == "" {
		path = "/mcp"
	}

	port := e.GetPrimaryPort()
	if port > 0 {
		return fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path)
	}

	return fmt.Sprintf("%s://%s%s", scheme, host, path)
}

// GetPrimaryPort returns the primary (first) host port
// Priority: port field > ports array > 0
func (e *MCPServerEntry) GetPrimaryPort() int {
	if e.Port > 0 {
		return e.Port
	}

	if len(e.Ports) == 0 {
		return 0
	}

	firstPort := e.Ports[0]
	var port int

	if strings.Contains(firstPort, ":") {
		parts := strings.Split(firstPort, ":")
		if _, err := fmt.Sscanf(parts[0], "%d", &port); err != nil {
			return 0
		}
		return port
	}

	if _, err := fmt.Sscanf(firstPort, "%d", &port); err != nil {
		return 0
	}
	return port
}

// FindAvailablePort finds the next available port starting from basePort
// It checks up to 100 ports after the base port
func FindAvailablePort(basePort int) int {
	for port := basePort; port < basePort+100; port++ {
		address := fmt.Sprintf("localhost:%d", port)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			continue
		}
		_ = listener.Close()
		return port
	}
	return basePort
}

// DefaultMCPConfig returns a default MCP configuration
func DefaultMCPConfig() *MCPConfig {
	return &MCPConfig{
		Enabled:               false,
		ConnectionTimeout:     30,
		DiscoveryTimeout:      30,
		LivenessProbeEnabled:  true,
		LivenessProbeInterval: 10,
		Servers:               []MCPServerEntry{},
	}
}

const (
	MCPFileName    = "mcp.yaml"
	DefaultMCPPath = ConfigDirName + "/" + MCPFileName
)
