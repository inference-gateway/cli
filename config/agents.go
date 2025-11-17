package config

// AgentsConfig represents the agents.yaml configuration file
type AgentsConfig struct {
	Agents []AgentEntry `yaml:"agents" mapstructure:"agents"`
}

// AgentEntry represents a single A2A agent configuration
type AgentEntry struct {
	Name        string            `yaml:"name" mapstructure:"name"`
	URL         string            `yaml:"url" mapstructure:"url"`
	OCI         string            `yaml:"oci,omitempty" mapstructure:"oci,omitempty"`
	Run         bool              `yaml:"run" mapstructure:"run"`
	Environment map[string]string `yaml:"environment,omitempty" mapstructure:"environment,omitempty"`
}

// DefaultAgentsConfig returns a default agents configuration
func DefaultAgentsConfig() *AgentsConfig {
	return &AgentsConfig{
		Agents: []AgentEntry{},
	}
}

const (
	AgentsFileName    = "agents.yaml"
	DefaultAgentsPath = ConfigDirName + "/" + AgentsFileName
)
