package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	configutils "github.com/inference-gateway/cli/config/utils"
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

	path string
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
	URL            string            `yaml:"url,omitempty" mapstructure:"url,omitempty"`
	Headers        map[string]string `yaml:"headers,omitempty" mapstructure:"headers,omitempty"`
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

// GetURL returns the server's URL: the url field when set, otherwise built
// from scheme, host, port and path.
func (e *MCPServerEntry) GetURL() string {
	if e.URL != "" {
		return e.URL
	}

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

// UnresolvedHeaders names the headers whose value is empty, as when the
// ${VAR} they expand from is unset. A server with any is skipped, so a
// process without the credential starts without the server instead of failing.
func (e *MCPServerEntry) UnresolvedHeaders() []string {
	var names []string
	for name, value := range e.Headers {
		if strings.TrimSpace(value) == "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
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

// UserMCPPath is ~/.infer/mcp.yaml, the servers of this machine's user.
func UserMCPPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ConfigDirName, MCPFileName)
}

// LoadMCP reads mcp.yaml to run with, its ${VAR} references expanded, or the
// defaults when the file is missing. Its entries cannot be saved, since that
// would write the variables' values over the references. LoadMCPForEdit can.
func LoadMCP(path string) (*MCPConfig, error) {
	return configutils.LoadYAML(path, "MCP", DefaultMCPConfig)
}

// LoadMCPForEdit reads mcp.yaml as written, so an entry changed and saved back
// keeps every ${VAR} reference in the file.
func LoadMCPForEdit(path string) (*MCPConfig, error) {
	cfg, err := configutils.LoadYAMLAsWritten(path, "MCP", DefaultMCPConfig)
	if err != nil {
		return nil, err
	}
	cfg.path = path
	return cfg, nil
}

func (c *MCPConfig) save() error {
	if c.path == "" {
		return fmt.Errorf("%s: %w", MCPFileName, errLoadedToRun)
	}
	return SaveMCP(c.path, c)
}

// SaveMCP writes the MCP configuration to disk, creating any missing
// parent directories.
func SaveMCP(path string, cfg *MCPConfig) error {
	return configutils.SaveYAML(path, "MCP", cfg)
}

func mcpServerName(e MCPServerEntry) string { return e.Name }

const mcpServerKind = "MCP server"

// CreateEntry implements CollectionConfig.
func (c *MCPConfig) CreateEntry(entry MCPServerEntry) error {
	next, err := appendEntry(c.Servers, entry, entry.Name, mcpServerName, mcpServerKind)
	if err != nil {
		return err
	}
	c.Servers = next
	return c.save()
}

// ReadEntry implements CollectionConfig.
func (c *MCPConfig) ReadEntry(name string) (*MCPServerEntry, error) {
	return findEntry(c.Servers, name, mcpServerName, mcpServerKind)
}

// UpdateEntry implements CollectionConfig.
func (c *MCPConfig) UpdateEntry(entry MCPServerEntry) error {
	next, err := replaceEntry(c.Servers, entry, entry.Name, mcpServerName, mcpServerKind)
	if err != nil {
		return err
	}
	c.Servers = next
	return c.save()
}

// DeleteEntry implements CollectionConfig.
func (c *MCPConfig) DeleteEntry(name string) error {
	next, err := removeEntry(c.Servers, name, mcpServerName, mcpServerKind)
	if err != nil {
		return err
	}
	c.Servers = next
	return c.save()
}

// ListEntries implements CollectionConfig.
func (c *MCPConfig) ListEntries() []MCPServerEntry { return c.Servers }
