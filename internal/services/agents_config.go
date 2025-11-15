package services

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// AgentsConfigServiceImpl implements the AgentsConfigService interface
type AgentsConfigServiceImpl struct{}

// NewAgentsConfigService creates a new agents config service
func NewAgentsConfigService() *AgentsConfigServiceImpl {
	return &AgentsConfigServiceImpl{}
}

// LoadConfig loads the agents configuration
func (s *AgentsConfigServiceImpl) LoadConfig() (*domain.AgentsConfig, error) {
	configAgents, err := config.LoadAgentsConfig()
	if err != nil {
		return nil, err
	}

	// Convert to domain type
	agents := make([]domain.AgentInfo, len(configAgents.Agents))
	for i, agent := range configAgents.Agents {
		agents[i] = domain.AgentInfo{
			Name: agent.Name,
			URL:  agent.URL,
			OCI:  agent.OCI,
			Run:  agent.Run,
		}
	}

	return &domain.AgentsConfig{
		Agents: agents,
	}, nil
}

// SaveConfig saves the agents configuration
func (s *AgentsConfigServiceImpl) SaveConfig(domainConfig *domain.AgentsConfig, path string) error {
	// Convert from domain type
	configAgents := make([]config.A2AAgentConfig, len(domainConfig.Agents))
	for i, agent := range domainConfig.Agents {
		configAgents[i] = config.A2AAgentConfig{
			Name: agent.Name,
			URL:  agent.URL,
			OCI:  agent.OCI,
			Run:  agent.Run,
		}
	}

	configData := &config.AgentsConfig{
		Agents: configAgents,
	}

	return config.SaveAgentsConfig(configData, path)
}

// GetConfiguredAgentURLs returns the URLs of all configured agents
func (s *AgentsConfigServiceImpl) GetConfiguredAgentURLs() []string {
	domainConfig, err := s.LoadConfig()
	if err != nil {
		return []string{}
	}

	urls := make([]string, len(domainConfig.Agents))
	for i, agent := range domainConfig.Agents {
		urls[i] = agent.URL
	}

	return urls
}

// AddAgent adds a new agent to the configuration
func (s *AgentsConfigServiceImpl) AddAgent(name, url, oci string, run bool) error {
	domainConfig, err := s.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check for duplicate names
	for _, agent := range domainConfig.Agents {
		if agent.Name == name {
			return fmt.Errorf("agent with name '%s' already exists", name)
		}
	}

	// Add new agent
	newAgent := domain.AgentInfo{
		Name: name,
		URL:  url,
		OCI:  oci,
		Run:  run,
	}
	domainConfig.Agents = append(domainConfig.Agents, newAgent)

	// Save to project-level config
	configPath := ".infer/agents.yaml"
	return s.SaveConfig(domainConfig, configPath)
}

// RemoveAgent removes an agent by name
func (s *AgentsConfigServiceImpl) RemoveAgent(name string) error {
	domainConfig, err := s.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Find and remove agent
	found := false
	for i, agent := range domainConfig.Agents {
		if agent.Name == name {
			domainConfig.Agents = append(domainConfig.Agents[:i], domainConfig.Agents[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("agent with name '%s' not found", name)
	}

	// Determine which config file to update
	configPath := ".infer/agents.yaml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, ".infer", "agents.yaml")
	}

	return s.SaveConfig(domainConfig, configPath)
}

// UpdateAgent updates an existing agent configuration
func (s *AgentsConfigServiceImpl) UpdateAgent(name string, url, oci *string, run *bool) error {
	domainConfig, err := s.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Find and update agent
	found := false
	for i, agent := range domainConfig.Agents {
		if agent.Name == name {
			if url != nil {
				agent.URL = *url
			}
			if oci != nil {
				agent.OCI = *oci
			}
			if run != nil {
				agent.Run = *run
			}
			domainConfig.Agents[i] = agent
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("agent with name '%s' not found", name)
	}

	// Determine which config file to update
	configPath := ".infer/agents.yaml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, ".infer", "agents.yaml")
	}

	return s.SaveConfig(domainConfig, configPath)
}

// ListAgents returns all configured agents
func (s *AgentsConfigServiceImpl) ListAgents() ([]domain.AgentInfo, error) {
	domainConfig, err := s.LoadConfig()
	if err != nil {
		return nil, err
	}

	return domainConfig.Agents, nil
}
