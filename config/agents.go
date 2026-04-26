package config

import (
	"strings"

	utils "github.com/inference-gateway/cli/config/utils"
)

// AgentsConfig represents the agents.yaml configuration file
type AgentsConfig struct {
	Agents []AgentEntry `yaml:"agents" mapstructure:"agents"`

	path string
}

var _ CollectionConfig[AgentEntry] = (*AgentsConfig)(nil)

// AgentEntry represents a single A2A agent configuration
type AgentEntry struct {
	Name         string            `yaml:"name" mapstructure:"name"`
	URL          string            `yaml:"url" mapstructure:"url"`
	ArtifactsURL string            `yaml:"artifacts_url,omitempty" mapstructure:"artifacts_url,omitempty"`
	OCI          string            `yaml:"oci,omitempty" mapstructure:"oci,omitempty"`
	Run          bool              `yaml:"run" mapstructure:"run"`
	Model        string            `yaml:"model,omitempty" mapstructure:"model,omitempty"`
	Environment  map[string]string `yaml:"environment,omitempty" mapstructure:"environment,omitempty"`
	Enabled      bool              `yaml:"enabled" mapstructure:"enabled"`
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

// LoadAgents reads agents.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "use
// defaults" without special-casing.
func LoadAgents(path string) (*AgentsConfig, error) {
	cfg, err := utils.LoadYAML(path, "agents", DefaultAgentsConfig)
	if err != nil {
		return nil, err
	}
	cfg.path = path
	return cfg, nil
}

// SaveAgents writes the agents configuration to disk, creating any
// missing parent directories.
func SaveAgents(path string, cfg *AgentsConfig) error {
	return utils.SaveYAML(path, "agents", cfg)
}

func agentName(e AgentEntry) string { return e.Name }

const agentKind = "agent"

// CreateEntry implements CollectionConfig.
func (c *AgentsConfig) CreateEntry(entry AgentEntry) error {
	next, err := appendEntry(c.Agents, entry, entry.Name, agentName, agentKind)
	if err != nil {
		return err
	}
	c.Agents = next
	return SaveAgents(c.path, c)
}

// ReadEntry implements CollectionConfig.
func (c *AgentsConfig) ReadEntry(name string) (*AgentEntry, error) {
	return findEntry(c.Agents, name, agentName, agentKind)
}

// UpdateEntry implements CollectionConfig.
func (c *AgentsConfig) UpdateEntry(entry AgentEntry) error {
	next, err := replaceEntry(c.Agents, entry, entry.Name, agentName, agentKind)
	if err != nil {
		return err
	}
	c.Agents = next
	return SaveAgents(c.path, c)
}

// DeleteEntry implements CollectionConfig.
func (c *AgentsConfig) DeleteEntry(name string) error {
	next, err := removeEntry(c.Agents, name, agentName, agentKind)
	if err != nil {
		return err
	}
	c.Agents = next
	return SaveAgents(c.path, c)
}

// ListEntries implements CollectionConfig.
func (c *AgentsConfig) ListEntries() []AgentEntry { return c.Agents }

// GetAgentURLs returns URLs of all configured agents.
func GetAgentURLs(path string) ([]string, error) {
	cfg, err := LoadAgents(path)
	if err != nil {
		return nil, err
	}
	agents := cfg.ListEntries()
	urls := make([]string, 0, len(agents))
	for _, agent := range agents {
		urls = append(urls, agent.URL)
	}
	return urls, nil
}
