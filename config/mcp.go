package config

// MCPConfig represents the mcp.yaml configuration file
type MCPConfig struct {
	Enabled           bool             `yaml:"enabled" mapstructure:"enabled"`
	ConnectionTimeout int              `yaml:"connection_timeout,omitempty" mapstructure:"connection_timeout,omitempty"`
	DiscoveryTimeout  int              `yaml:"discovery_timeout,omitempty" mapstructure:"discovery_timeout,omitempty"`
	Servers           []MCPServerEntry `yaml:"servers" mapstructure:"servers"`
}

// MCPServerEntry represents a single MCP server configuration
type MCPServerEntry struct {
	Name         string   `yaml:"name" mapstructure:"name"`
	URL          string   `yaml:"url" mapstructure:"url"`
	Enabled      bool     `yaml:"enabled" mapstructure:"enabled"`
	Timeout      int      `yaml:"timeout,omitempty" mapstructure:"timeout,omitempty"`
	Description  string   `yaml:"description,omitempty" mapstructure:"description,omitempty"`
	IncludeTools []string `yaml:"include_tools,omitempty" mapstructure:"include_tools,omitempty"`
	ExcludeTools []string `yaml:"exclude_tools,omitempty" mapstructure:"exclude_tools,omitempty"`
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

// DefaultMCPConfig returns a default MCP configuration
func DefaultMCPConfig() *MCPConfig {
	return &MCPConfig{
		Enabled:           false,
		ConnectionTimeout: 30,
		DiscoveryTimeout:  30,
		Servers:           []MCPServerEntry{},
	}
}

const (
	MCPFileName    = "mcp.yaml"
	DefaultMCPPath = ConfigDirName + "/" + MCPFileName
)
