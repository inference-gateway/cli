package services

import (
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// MockDockerService for testing
type MockDockerService struct {
	isAvailable bool
	containers  map[string]*domain.ContainerStatus
}

func NewMockDockerService() *MockDockerService {
	return &MockDockerService{
		isAvailable: true,
		containers:  make(map[string]*domain.ContainerStatus),
	}
}

func (m *MockDockerService) StartContainer(agent domain.AgentDefinition) (string, error) {
	containerID := "mock-container-" + agent.Name
	m.containers[containerID] = &domain.ContainerStatus{
		ID:      containerID,
		Name:    agent.Name,
		State:   "running",
		Health:  "healthy",
		Created: "2024-01-01T00:00:00Z",
	}
	return containerID, nil
}

func (m *MockDockerService) StopContainer(containerID string) error {
	delete(m.containers, containerID)
	return nil
}

func (m *MockDockerService) GetContainerStatus(containerID string) (*domain.ContainerStatus, error) {
	if status, exists := m.containers[containerID]; exists {
		return status, nil
	}
	return nil, domain.ErrContainerNotFound
}

func (m *MockDockerService) IsDockerAvailable() bool {
	return m.isAvailable
}

// TestableAgentConfigService wraps the service to allow overriding config path
type TestableAgentConfigService struct {
	*AgentConfigServiceImpl
	testConfigPath string
}

func (s *TestableAgentConfigService) GetConfigPath() (string, error) {
	if s.testConfigPath != "" {
		return s.testConfigPath, nil
	}
	return s.AgentConfigServiceImpl.GetConfigPath()
}

func TestAgentConfigService_AddAgent(t *testing.T) {
	// Create temporary directory for test
	tempDir := t.TempDir()

	// Create test config
	cfg := &config.Config{}
	dockerService := NewMockDockerService()
	baseService := NewAgentConfigService(cfg, dockerService).(*AgentConfigServiceImpl)

	// Create testable wrapper
	tempConfigPath := filepath.Join(tempDir, "agents.yaml")
	service := &TestableAgentConfigService{
		AgentConfigServiceImpl: baseService,
		testConfigPath:         tempConfigPath,
	}

	// Test adding an agent
	agent := domain.AgentDefinition{
		Name:        "test-agent",
		URL:         "http://localhost:8080",
		OCI:         "test-agent:latest",
		Run:         true,
		Description: "Test agent",
		Enabled:     true,
		Environment: map[string]string{
			"API_KEY": "test-key",
		},
	}

	err := service.AddAgent(agent)
	if err != nil {
		t.Fatalf("Failed to add agent: %v", err)
	}

	// Verify agent was added
	agents, err := service.ListAgents()
	if err != nil {
		t.Fatalf("Failed to list agents: %v", err)
	}

	if len(agents) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(agents))
	}

	if agents[0].Name != "test-agent" {
		t.Fatalf("Expected agent name 'test-agent', got %s", agents[0].Name)
	}

	if agents[0].URL != "http://localhost:8080" {
		t.Fatalf("Expected agent URL 'http://localhost:8080', got %s", agents[0].URL)
	}
}

func TestAgentConfigService_GetAgent(t *testing.T) {
	// Create temporary directory for test
	tempDir := t.TempDir()

	// Create test config
	cfg := &config.Config{}
	dockerService := NewMockDockerService()
	baseService := NewAgentConfigService(cfg, dockerService).(*AgentConfigServiceImpl)

	// Create testable wrapper
	tempConfigPath := filepath.Join(tempDir, "agents.yaml")
	service := &TestableAgentConfigService{
		AgentConfigServiceImpl: baseService,
		testConfigPath:         tempConfigPath,
	}

	// Add an agent
	agent := domain.AgentDefinition{
		Name:        "test-agent",
		URL:         "http://localhost:8080",
		Description: "Test agent",
		Enabled:     true,
	}

	err := service.AddAgent(agent)
	if err != nil {
		t.Fatalf("Failed to add agent: %v", err)
	}

	// Test getting the agent
	retrievedAgent, err := service.GetAgent("test-agent")
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}

	if retrievedAgent.Name != "test-agent" {
		t.Fatalf("Expected agent name 'test-agent', got %s", retrievedAgent.Name)
	}

	// Test getting non-existent agent
	_, err = service.GetAgent("non-existent")
	if err == nil {
		t.Fatalf("Expected error when getting non-existent agent")
	}
}

func TestAgentConfigService_RemoveAgent(t *testing.T) {
	// Create temporary directory for test
	tempDir := t.TempDir()

	// Create test config
	cfg := &config.Config{}
	dockerService := NewMockDockerService()
	baseService := NewAgentConfigService(cfg, dockerService).(*AgentConfigServiceImpl)

	// Create testable wrapper
	tempConfigPath := filepath.Join(tempDir, "agents.yaml")
	service := &TestableAgentConfigService{
		AgentConfigServiceImpl: baseService,
		testConfigPath:         tempConfigPath,
	}

	// Add an agent
	agent := domain.AgentDefinition{
		Name:        "test-agent",
		URL:         "http://localhost:8080",
		Description: "Test agent",
		Enabled:     true,
	}

	err := service.AddAgent(agent)
	if err != nil {
		t.Fatalf("Failed to add agent: %v", err)
	}

	// Verify agent exists
	agents, err := service.ListAgents()
	if err != nil {
		t.Fatalf("Failed to list agents: %v", err)
	}

	if len(agents) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(agents))
	}

	// Remove the agent
	err = service.RemoveAgent("test-agent")
	if err != nil {
		t.Fatalf("Failed to remove agent: %v", err)
	}

	// Verify agent was removed
	agents, err = service.ListAgents()
	if err != nil {
		t.Fatalf("Failed to list agents: %v", err)
	}

	if len(agents) != 0 {
		t.Fatalf("Expected 0 agents, got %d", len(agents))
	}

	// Test removing non-existent agent
	err = service.RemoveAgent("non-existent")
	if err == nil {
		t.Fatalf("Expected error when removing non-existent agent")
	}
}

func TestAgentConfigService_EmptyConfig(t *testing.T) {
	// Create test config
	cfg := &config.Config{}
	dockerService := NewMockDockerService()
	baseService := NewAgentConfigService(cfg, dockerService).(*AgentConfigServiceImpl)

	// Create testable wrapper with non-existent path
	service := &TestableAgentConfigService{
		AgentConfigServiceImpl: baseService,
		testConfigPath:         "/tmp/non-existent-agents.yaml",
	}

	// Test listing agents from empty/non-existent config
	agents, err := service.ListAgents()
	if err != nil {
		t.Fatalf("Failed to list agents from empty config: %v", err)
	}

	if len(agents) != 0 {
		t.Fatalf("Expected 0 agents from empty config, got %d", len(agents))
	}
}

func TestAgentConfigService_StartStopAgent(t *testing.T) {
	// Create temporary directory for test
	tempDir := t.TempDir()

	// Create test config
	cfg := &config.Config{}
	dockerService := NewMockDockerService()
	baseService := NewAgentConfigService(cfg, dockerService).(*AgentConfigServiceImpl)

	// Create testable wrapper
	tempConfigPath := filepath.Join(tempDir, "agents.yaml")
	service := &TestableAgentConfigService{
		AgentConfigServiceImpl: baseService,
		testConfigPath:         tempConfigPath,
	}

	// Add a local agent
	agent := domain.AgentDefinition{
		Name:        "test-agent",
		URL:         "http://localhost:8080",
		OCI:         "test-agent:latest",
		Run:         true,
		Description: "Test agent",
		Enabled:     true,
	}

	err := service.AddAgent(agent)
	if err != nil {
		t.Fatalf("Failed to add agent: %v", err)
	}

	// Test starting the agent
	err = service.StartAgent("test-agent")
	if err != nil {
		t.Fatalf("Failed to start agent: %v", err)
	}

	// Verify agent is running
	status, err := service.GetAgentStatus("test-agent")
	if err != nil {
		t.Fatalf("Failed to get agent status: %v", err)
	}

	if !status.Running {
		t.Fatalf("Expected agent to be running")
	}

	// Test stopping the agent
	err = service.StopAgent("test-agent")
	if err != nil {
		t.Fatalf("Failed to stop agent: %v", err)
	}

	// Verify agent is stopped
	status, err = service.GetAgentStatus("test-agent")
	if err != nil {
		t.Fatalf("Failed to get agent status: %v", err)
	}

	if status.Running {
		t.Fatalf("Expected agent to be stopped")
	}
}
