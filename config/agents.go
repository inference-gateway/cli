package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	AgentsFileName = "agents.yaml"
)

// A2AAgentConfig represents a single A2A agent configuration
type A2AAgentConfig struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
	OCI  string `yaml:"oci,omitempty" json:"oci,omitempty"`
	Run  bool   `yaml:"run" json:"run"`
}

// AgentsConfig represents the agents configuration file structure
type AgentsConfig struct {
	Agents []A2AAgentConfig `yaml:"agents" json:"agents"`
}

// LoadAgentsConfig loads agents configuration with priority: project -> userspace
func LoadAgentsConfig() (*AgentsConfig, error) {
	// Try project level first
	if projectConfig, err := loadAgentsConfigFromPath(".infer/agents.yaml"); err == nil {
		return projectConfig, nil
	}

	// Try userspace
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return &AgentsConfig{}, nil // Return empty config if no home dir
	}

	userspacePath := filepath.Join(homeDir, ".infer", "agents.yaml")
	if userspaceConfig, err := loadAgentsConfigFromPath(userspacePath); err == nil {
		return userspaceConfig, nil
	}

	// Return empty config if no files found
	return &AgentsConfig{}, nil
}

// loadAgentsConfigFromPath loads agents config from a specific file path
func loadAgentsConfigFromPath(path string) (*AgentsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config AgentsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse agents config: %w", err)
	}

	return &config, nil
}

// SaveAgentsConfig saves agents configuration to the specified path
func SaveAgentsConfig(config *AgentsConfig, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal agents config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write agents config: %w", err)
	}

	return nil
}

// GetAgentByName returns an agent configuration by name
func (ac *AgentsConfig) GetAgentByName(name string) (*A2AAgentConfig, error) {
	for _, agent := range ac.Agents {
		if agent.Name == name {
			return &agent, nil
		}
	}
	return nil, fmt.Errorf("agent with name '%s' not found", name)
}

// AddAgent adds a new agent to the configuration
func (ac *AgentsConfig) AddAgent(agent A2AAgentConfig) error {
	// Check for duplicate names
	if _, err := ac.GetAgentByName(agent.Name); err == nil {
		return fmt.Errorf("agent with name '%s' already exists", agent.Name)
	}

	ac.Agents = append(ac.Agents, agent)
	return nil
}

// RemoveAgent removes an agent by name
func (ac *AgentsConfig) RemoveAgent(name string) error {
	for i, agent := range ac.Agents {
		if agent.Name == name {
			ac.Agents = append(ac.Agents[:i], ac.Agents[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("agent with name '%s' not found", name)
}

// UpdateAgent updates an existing agent configuration
func (ac *AgentsConfig) UpdateAgent(name string, updatedAgent A2AAgentConfig) error {
	for i, agent := range ac.Agents {
		if agent.Name == name {
			// Preserve the original name if the updated agent has a different name
			if updatedAgent.Name != name {
				updatedAgent.Name = name
			}
			ac.Agents[i] = updatedAgent
			return nil
		}
	}
	return fmt.Errorf("agent with name '%s' not found", name)
}

// GetAgentURLs returns a slice of agent URLs for backward compatibility
func (ac *AgentsConfig) GetAgentURLs() []string {
	urls := make([]string, 0, len(ac.Agents))
	for _, agent := range ac.Agents {
		urls = append(urls, agent.URL)
	}
	return urls
}
