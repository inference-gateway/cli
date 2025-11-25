package services

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	yaml "gopkg.in/yaml.v3"
)

// AgentsConfigService manages the agents.yaml configuration
type AgentsConfigService struct {
	configPath string
}

// NewAgentsConfigService creates a new agents config service
func NewAgentsConfigService(configPath string) *AgentsConfigService {
	return &AgentsConfigService{
		configPath: configPath,
	}
}

// Load loads the agents configuration from the file
func (s *AgentsConfigService) Load() (*config.AgentsConfig, error) {
	if _, err := os.Stat(s.configPath); os.IsNotExist(err) {
		logger.Info("Agents config file does not exist, returning default", "path", s.configPath)
		return config.DefaultAgentsConfig(), nil
	}

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents config: %w", err)
	}

	expandedData := os.ExpandEnv(string(data))

	var agentsConfig config.AgentsConfig
	if err := yaml.Unmarshal([]byte(expandedData), &agentsConfig); err != nil {
		return nil, fmt.Errorf("failed to parse agents config: %w", err)
	}

	return &agentsConfig, nil
}

// Save saves the agents configuration to the file
func (s *AgentsConfigService) Save(cfg *config.AgentsConfig) error {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal agents config: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close encoder: %w", err)
	}

	data := buf.Bytes()

	configDir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(s.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write agents config: %w", err)
	}

	logger.Info("Agents config saved", "path", s.configPath)
	return nil
}

// AddAgent adds a new agent to the configuration
func (s *AgentsConfigService) AddAgent(agent config.AgentEntry) error {
	cfg, err := s.Load()
	if err != nil {
		return err
	}

	for _, existing := range cfg.Agents {
		if existing.Name == agent.Name {
			return fmt.Errorf("agent with name '%s' already exists", agent.Name)
		}
	}

	cfg.Agents = append(cfg.Agents, agent)
	return s.Save(cfg)
}

// UpdateAgent updates an existing agent in the configuration
func (s *AgentsConfigService) UpdateAgent(agent config.AgentEntry) error {
	cfg, err := s.Load()
	if err != nil {
		return err
	}

	found := false
	for i, existing := range cfg.Agents {
		if existing.Name == agent.Name {
			cfg.Agents[i] = agent
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("agent with name '%s' not found", agent.Name)
	}

	return s.Save(cfg)
}

// RemoveAgent removes an agent by name
func (s *AgentsConfigService) RemoveAgent(name string) error {
	cfg, err := s.Load()
	if err != nil {
		return err
	}

	found := false
	newAgents := make([]config.AgentEntry, 0, len(cfg.Agents))
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
	return s.Save(cfg)
}

// ListAgents returns all configured agents
func (s *AgentsConfigService) ListAgents() ([]config.AgentEntry, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	return cfg.Agents, nil
}

// GetAgent returns a specific agent by name
func (s *AgentsConfigService) GetAgent(name string) (*config.AgentEntry, error) {
	cfg, err := s.Load()
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

// GetAgentURLs returns URLs of all configured agents
func (s *AgentsConfigService) GetAgentURLs() ([]string, error) {
	agents, err := s.ListAgents()
	if err != nil {
		return nil, err
	}

	urls := make([]string, 0, len(agents))
	for _, agent := range agents {
		urls = append(urls, agent.URL)
	}
	return urls, nil
}
