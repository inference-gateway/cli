package config

import "strings"

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
	Model       string            `yaml:"model,omitempty" mapstructure:"model,omitempty"` // Format: "provider/model" e.g., "anthropic/claude-3-sonnet"
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

// ParseModel parses a model string in the format "provider/model" and returns
// the provider and model separately. If the format is invalid, returns empty strings.
func ParseModel(model string) (provider, modelName string) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

// GetEnvironmentWithModel returns the environment variables with model-related
// variables added if a model is specified.
func (a *AgentEntry) GetEnvironmentWithModel() map[string]string {
	env := make(map[string]string)

	for k, v := range a.Environment {
		env[k] = v
	}

	if a.Model != "" {
		provider, modelName := ParseModel(a.Model)
		if provider != "" && modelName != "" {
			env["A2A_AGENT_CLIENT_PROVIDER"] = provider
			env["A2A_AGENT_CLIENT_MODEL"] = modelName
		}
	}

	return env
}
