package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

const (
	AgentsFileName = "agents.yaml"
)

type AgentConfigServiceImpl struct {
	config        *config.Config
	dockerService domain.DockerService
	runningAgents map[string]string // map[agentName]containerID
	agentsMutex   sync.RWMutex
	configMutex   sync.RWMutex
}

func NewAgentConfigService(cfg *config.Config, dockerService domain.DockerService) domain.AgentConfigService {
	return &AgentConfigServiceImpl{
		config:        cfg,
		dockerService: dockerService,
		runningAgents: make(map[string]string),
	}
}

func (s *AgentConfigServiceImpl) LoadAgents() (*domain.AgentConfigFile, error) {
	s.configMutex.RLock()
	defer s.configMutex.RUnlock()

	configPath, err := s.GetConfigPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get config path: %w", err)
	}

	// If file doesn't exist, return empty config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Debug("Agents config file doesn't exist, returning empty config", "path", configPath)
		return &domain.AgentConfigFile{
			Agents: []domain.AgentDefinition{},
		}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents config file: %w", err)
	}

	var agentConfig domain.AgentConfigFile
	if err := yaml.Unmarshal(data, &agentConfig); err != nil {
		return nil, fmt.Errorf("failed to parse agents config file: %w", err)
	}

	return &agentConfig, nil
}

func (s *AgentConfigServiceImpl) SaveAgents(agentConfig *domain.AgentConfigFile) error {
	s.configMutex.Lock()
	defer s.configMutex.Unlock()

	configPath, err := s.GetConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	// Ensure the directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(agentConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal agents config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write agents config file: %w", err)
	}

	logger.Info("Agents configuration saved", "path", configPath, "agent_count", len(agentConfig.Agents))
	return nil
}

func (s *AgentConfigServiceImpl) AddAgent(agent domain.AgentDefinition) error {
	agentConfig, err := s.LoadAgents()
	if err != nil {
		return fmt.Errorf("failed to load existing agents: %w", err)
	}

	// Check if agent with same name already exists
	for i, existing := range agentConfig.Agents {
		if existing.Name == agent.Name {
			// Replace existing agent
			agentConfig.Agents[i] = agent
			logger.Info("Replacing existing agent", "name", agent.Name)
			return s.SaveAgents(agentConfig)
		}
	}

	// Add new agent
	agentConfig.Agents = append(agentConfig.Agents, agent)
	logger.Info("Adding new agent", "name", agent.Name, "url", agent.URL)

	return s.SaveAgents(agentConfig)
}

func (s *AgentConfigServiceImpl) RemoveAgent(name string) error {
	agentConfig, err := s.LoadAgents()
	if err != nil {
		return fmt.Errorf("failed to load existing agents: %w", err)
	}

	// Stop the agent if it's running
	if s.dockerService.IsDockerAvailable() {
		if err := s.StopAgent(name); err != nil {
			logger.Warn("Failed to stop agent before removal", "name", name, "error", err)
		}
	}

	// Find and remove the agent
	for i, agent := range agentConfig.Agents {
		if agent.Name == name {
			agentConfig.Agents = append(agentConfig.Agents[:i], agentConfig.Agents[i+1:]...)
			logger.Info("Removing agent", "name", name)
			return s.SaveAgents(agentConfig)
		}
	}

	return fmt.Errorf("agent not found: %s", name)
}

func (s *AgentConfigServiceImpl) GetAgent(name string) (*domain.AgentDefinition, error) {
	agentConfig, err := s.LoadAgents()
	if err != nil {
		return nil, fmt.Errorf("failed to load agents: %w", err)
	}

	for _, agent := range agentConfig.Agents {
		if agent.Name == name {
			return &agent, nil
		}
	}

	return nil, fmt.Errorf("agent not found: %s", name)
}

func (s *AgentConfigServiceImpl) ListAgents() ([]domain.AgentDefinition, error) {
	agentConfig, err := s.LoadAgents()
	if err != nil {
		return nil, fmt.Errorf("failed to load agents: %w", err)
	}

	return agentConfig.Agents, nil
}

func (s *AgentConfigServiceImpl) GetConfigPath() (string, error) {
	// Try project-specific config first
	projectConfigPath := filepath.Join(config.ConfigDirName, AgentsFileName)
	if _, err := os.Stat(projectConfigPath); err == nil {
		return filepath.Abs(projectConfigPath)
	}

	// Fall back to user config directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	userConfigPath := filepath.Join(homeDir, config.ConfigDirName, AgentsFileName)
	return userConfigPath, nil
}

func (s *AgentConfigServiceImpl) StartAgent(name string) error {
	if !s.dockerService.IsDockerAvailable() {
		return fmt.Errorf("Docker is not available")
	}

	agent, err := s.GetAgent(name)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	if !agent.Run {
		return fmt.Errorf("agent %s is not configured for local execution", name)
	}

	if !agent.Enabled {
		return fmt.Errorf("agent %s is disabled", name)
	}

	// Check if already running
	s.agentsMutex.RLock()
	containerID, isRunning := s.runningAgents[name]
	s.agentsMutex.RUnlock()

	if isRunning {
		// Verify container is actually running
		status, err := s.dockerService.GetContainerStatus(containerID)
		if err == nil && status.State == "running" {
			return fmt.Errorf("agent %s is already running", name)
		}
		// Remove stale entry
		s.agentsMutex.Lock()
		delete(s.runningAgents, name)
		s.agentsMutex.Unlock()
	}

	containerID, err = s.dockerService.StartContainer(*agent)
	if err != nil {
		return fmt.Errorf("failed to start container for agent %s: %w", name, err)
	}

	s.agentsMutex.Lock()
	s.runningAgents[name] = containerID
	s.agentsMutex.Unlock()

	logger.Info("Started agent", "name", name, "container_id", containerID)
	return nil
}

func (s *AgentConfigServiceImpl) StopAgent(name string) error {
	if !s.dockerService.IsDockerAvailable() {
		return fmt.Errorf("Docker is not available")
	}

	s.agentsMutex.RLock()
	containerID, isRunning := s.runningAgents[name]
	s.agentsMutex.RUnlock()

	if !isRunning {
		return fmt.Errorf("agent %s is not running", name)
	}

	if err := s.dockerService.StopContainer(containerID); err != nil {
		return fmt.Errorf("failed to stop container for agent %s: %w", name, err)
	}

	s.agentsMutex.Lock()
	delete(s.runningAgents, name)
	s.agentsMutex.Unlock()

	logger.Info("Stopped agent", "name", name, "container_id", containerID)
	return nil
}

func (s *AgentConfigServiceImpl) GetAgentStatus(name string) (domain.AgentStatus, error) {
	agent, err := s.GetAgent(name)
	if err != nil {
		return domain.AgentStatus{}, fmt.Errorf("failed to get agent: %w", err)
	}

	status := domain.AgentStatus{
		Name:     name,
		Running:  false,
		URL:      agent.URL,
		Metadata: agent.Metadata,
	}

	if !agent.Run || !s.dockerService.IsDockerAvailable() {
		// Agent is not configured for local execution or Docker is not available
		if agent.Enabled {
			status.Running = true // Assume it's running externally
			status.Health = "unknown"
		}
		return status, nil
	}

	s.agentsMutex.RLock()
	containerID, isRunning := s.runningAgents[name]
	s.agentsMutex.RUnlock()

	if !isRunning {
		return status, nil
	}

	containerStatus, err := s.dockerService.GetContainerStatus(containerID)
	if err != nil {
		return status, fmt.Errorf("failed to get container status: %w", err)
	}

	status.Running = containerStatus.State == "running"
	status.Container = containerID
	status.Health = containerStatus.Health

	return status, nil
}
