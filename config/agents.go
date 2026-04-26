package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// AgentsConfig represents the agents.yaml configuration file
type AgentsConfig struct {
	Agents []AgentEntry `yaml:"agents" mapstructure:"agents"`
}

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
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultAgentsConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents config: %w", err)
	}

	expandedData := os.ExpandEnv(string(data))

	var cfg AgentsConfig
	if err := yaml.Unmarshal([]byte(expandedData), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse agents config: %w", err)
	}

	return &cfg, nil
}

// SaveAgents writes the agents configuration to disk, creating any
// missing parent directories.
func SaveAgents(path string, cfg *AgentsConfig) error {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal agents config: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close encoder: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write agents config: %w", err)
	}

	return nil
}

// AddAgent adds a new agent to the configuration file.
func AddAgent(path string, agent AgentEntry) error {
	cfg, err := LoadAgents(path)
	if err != nil {
		return err
	}

	for _, existing := range cfg.Agents {
		if existing.Name == agent.Name {
			return fmt.Errorf("agent with name '%s' already exists", agent.Name)
		}
	}

	cfg.Agents = append(cfg.Agents, agent)
	return SaveAgents(path, cfg)
}

// UpdateAgent updates an existing agent by name.
func UpdateAgent(path string, agent AgentEntry) error {
	cfg, err := LoadAgents(path)
	if err != nil {
		return err
	}

	for i, existing := range cfg.Agents {
		if existing.Name == agent.Name {
			cfg.Agents[i] = agent
			return SaveAgents(path, cfg)
		}
	}

	return fmt.Errorf("agent with name '%s' not found", agent.Name)
}

// RemoveAgent removes an agent by name.
func RemoveAgent(path, name string) error {
	cfg, err := LoadAgents(path)
	if err != nil {
		return err
	}

	found := false
	newAgents := make([]AgentEntry, 0, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		if agent.Name != name {
			newAgents = append(newAgents, agent)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("agent with name '%s' not found", name)
	}

	cfg.Agents = newAgents
	return SaveAgents(path, cfg)
}

// ListAgents returns all configured agents.
func ListAgents(path string) ([]AgentEntry, error) {
	cfg, err := LoadAgents(path)
	if err != nil {
		return nil, err
	}
	return cfg.Agents, nil
}

// GetAgent returns a single agent entry by name.
func GetAgent(path, name string) (*AgentEntry, error) {
	cfg, err := LoadAgents(path)
	if err != nil {
		return nil, err
	}

	for _, agent := range cfg.Agents {
		if agent.Name == name {
			return &agent, nil
		}
	}

	return nil, fmt.Errorf("agent with name '%s' not found", name)
}

// GetAgentURLs returns URLs of all configured agents.
func GetAgentURLs(path string) ([]string, error) {
	agents, err := ListAgents(path)
	if err != nil {
		return nil, err
	}

	urls := make([]string, 0, len(agents))
	for _, agent := range agents {
		urls = append(urls, agent.URL)
	}
	return urls, nil
}
