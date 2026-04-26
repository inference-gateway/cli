package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// MCPConfig represents the mcp.yaml configuration file
type MCPConfig struct {
	Enabled               bool             `yaml:"enabled" mapstructure:"enabled"`
	ConnectionTimeout     int              `yaml:"connection_timeout,omitempty" mapstructure:"connection_timeout,omitempty"`
	DiscoveryTimeout      int              `yaml:"discovery_timeout,omitempty" mapstructure:"discovery_timeout,omitempty"`
	LivenessProbeEnabled  bool             `yaml:"liveness_probe_enabled,omitempty" mapstructure:"liveness_probe_enabled,omitempty"`
	LivenessProbeInterval int              `yaml:"liveness_probe_interval,omitempty" mapstructure:"liveness_probe_interval,omitempty"`
	MaxRetries            int              `yaml:"max_retries,omitempty" mapstructure:"max_retries,omitempty"`
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

// DefaultMCPConfig returns a default MCP configuration
func DefaultMCPConfig() *MCPConfig {
	return &MCPConfig{
		Enabled:               false,
		ConnectionTimeout:     30,
		DiscoveryTimeout:      30,
		LivenessProbeEnabled:  true,
		LivenessProbeInterval: 10,
		MaxRetries:            10,
		Servers:               []MCPServerEntry{},
	}
}

const (
	MCPFileName    = "mcp.yaml"
	DefaultMCPPath = ConfigDirName + "/" + MCPFileName
)

// LoadMCP reads mcp.yaml from disk. When the file is missing it returns
// the in-code defaults so callers can treat absence as "use defaults"
// without special-casing.
func LoadMCP(path string) (*MCPConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultMCPConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read MCP config: %w", err)
	}

	expandedData := os.ExpandEnv(string(data))

	var cfg MCPConfig
	if err := yaml.Unmarshal([]byte(expandedData), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse MCP config: %w", err)
	}

	return &cfg, nil
}

// SaveMCP writes the MCP configuration to disk, creating any missing
// parent directories.
func SaveMCP(path string, cfg *MCPConfig) error {
	var buf bytes.Buffer
	buf.WriteString("---\n")

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close encoder: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write MCP config: %w", err)
	}

	return nil
}

// AddMCPServer adds a new MCP server entry to the configuration file.
func AddMCPServer(path string, server MCPServerEntry) error {
	cfg, err := LoadMCP(path)
	if err != nil {
		return err
	}

	for _, existing := range cfg.Servers {
		if existing.Name == server.Name {
			return fmt.Errorf("MCP server with name '%s' already exists", server.Name)
		}
	}

	cfg.Servers = append(cfg.Servers, server)
	return SaveMCP(path, cfg)
}

// UpdateMCPServer updates an existing MCP server entry by name.
func UpdateMCPServer(path string, server MCPServerEntry) error {
	cfg, err := LoadMCP(path)
	if err != nil {
		return err
	}

	for i, existing := range cfg.Servers {
		if existing.Name == server.Name {
			cfg.Servers[i] = server
			return SaveMCP(path, cfg)
		}
	}

	return fmt.Errorf("MCP server with name '%s' not found", server.Name)
}

// RemoveMCPServer removes an MCP server entry by name.
func RemoveMCPServer(path, name string) error {
	cfg, err := LoadMCP(path)
	if err != nil {
		return err
	}

	found := false
	newServers := make([]MCPServerEntry, 0, len(cfg.Servers))
	for _, server := range cfg.Servers {
		if server.Name != name {
			newServers = append(newServers, server)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("MCP server with name '%s' not found", name)
	}

	cfg.Servers = newServers
	return SaveMCP(path, cfg)
}

// ListMCPServers returns all configured MCP servers.
func ListMCPServers(path string) ([]MCPServerEntry, error) {
	cfg, err := LoadMCP(path)
	if err != nil {
		return nil, err
	}
	return cfg.Servers, nil
}

// GetMCPServer returns a single MCP server entry by name.
func GetMCPServer(path, name string) (*MCPServerEntry, error) {
	cfg, err := LoadMCP(path)
	if err != nil {
		return nil, err
	}

	for _, server := range cfg.Servers {
		if server.Name == name {
			return &server, nil
		}
	}

	return nil, fmt.Errorf("MCP server with name '%s' not found", name)
}

// MergeMCP merges an optional mcp.yaml config on top of a base config.
// Optional values take precedence; servers from both are combined and
// optional entries override base entries with the same name.
func MergeMCP(base *MCPConfig, optional *MCPConfig) *MCPConfig {
	if optional == nil {
		return base
	}

	merged := &MCPConfig{
		Enabled:               optional.Enabled || base.Enabled,
		ConnectionTimeout:     base.ConnectionTimeout,
		DiscoveryTimeout:      base.DiscoveryTimeout,
		LivenessProbeEnabled:  base.LivenessProbeEnabled,
		LivenessProbeInterval: base.LivenessProbeInterval,
		MaxRetries:            base.MaxRetries,
		Servers:               make([]MCPServerEntry, 0),
	}

	if optional.ConnectionTimeout > 0 {
		merged.ConnectionTimeout = optional.ConnectionTimeout
	}
	if optional.DiscoveryTimeout > 0 {
		merged.DiscoveryTimeout = optional.DiscoveryTimeout
	}
	if optional.LivenessProbeInterval > 0 {
		merged.LivenessProbeInterval = optional.LivenessProbeInterval
	}
	if optional.MaxRetries > 0 {
		merged.MaxRetries = optional.MaxRetries
	}
	if optional.LivenessProbeEnabled {
		merged.LivenessProbeEnabled = true
	}

	serverMap := make(map[string]MCPServerEntry)
	for _, server := range base.Servers {
		serverMap[server.Name] = server
	}
	for _, server := range optional.Servers {
		serverMap[server.Name] = server
	}

	for _, server := range serverMap {
		merged.Servers = append(merged.Servers, server)
	}

	return merged
}
