package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentsConfigService_LoadConfig(t *testing.T) {
	service := NewAgentsConfigService()

	// Test with no config file
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	config, err := service.LoadConfig()
	require.NoError(t, err)
	assert.Empty(t, config.Agents)
}

func TestAgentsConfigService_SaveAndLoadConfig(t *testing.T) {
	service := NewAgentsConfigService()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, ".infer", "agents.yaml")

	// Create test config
	testConfig := &domain.AgentsConfig{
		Agents: []domain.AgentInfo{
			{
				Name: "test-agent",
				URL:  "http://localhost:8081",
				OCI:  "docker.io/test/agent:latest",
				Run:  true,
			},
		},
	}

	// Save config
	err := service.SaveConfig(testConfig, configPath)
	require.NoError(t, err)

	// Change directory and load config
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	// Load config (from specific path using the config package)
	loadedConfig, err := service.LoadConfig()
	require.NoError(t, err)

	assert.Len(t, loadedConfig.Agents, 1)
	assert.Equal(t, "test-agent", loadedConfig.Agents[0].Name)
	assert.Equal(t, "http://localhost:8081", loadedConfig.Agents[0].URL)
	assert.Equal(t, "docker.io/test/agent:latest", loadedConfig.Agents[0].OCI)
	assert.True(t, loadedConfig.Agents[0].Run)
}

func TestAgentsConfigService_GetConfiguredAgentURLs(t *testing.T) {
	service := NewAgentsConfigService()

	// Test with no config
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	urls := service.GetConfiguredAgentURLs()
	assert.Empty(t, urls)
}

func TestAgentsConfigService_AddAgent(t *testing.T) {
	service := NewAgentsConfigService()
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	// Add first agent
	err := service.AddAgent("test-agent", "http://localhost:8081", "docker.io/test/agent:latest", true)
	require.NoError(t, err)

	// Verify agent was added
	urls := service.GetConfiguredAgentURLs()
	assert.Equal(t, []string{"http://localhost:8081"}, urls)

	// Try to add duplicate
	err = service.AddAgent("test-agent", "http://localhost:8082", "", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestAgentsConfigService_RemoveAgent(t *testing.T) {
	service := NewAgentsConfigService()
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	// Add agents
	err := service.AddAgent("agent1", "http://localhost:8081", "", false)
	require.NoError(t, err)
	err = service.AddAgent("agent2", "http://localhost:8082", "", false)
	require.NoError(t, err)

	// Remove existing agent
	err = service.RemoveAgent("agent1")
	require.NoError(t, err)

	// Verify only agent2 remains
	urls := service.GetConfiguredAgentURLs()
	assert.Equal(t, []string{"http://localhost:8082"}, urls)

	// Try to remove non-existing agent
	err = service.RemoveAgent("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAgentsConfigService_UpdateAgent(t *testing.T) {
	service := NewAgentsConfigService()
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	// Add agent
	err := service.AddAgent("test-agent", "http://localhost:8081", "", false)
	require.NoError(t, err)

	// Update agent
	newURL := "http://localhost:9081"
	newOCI := "docker.io/updated/agent:latest"
	newRun := true
	err = service.UpdateAgent("test-agent", &newURL, &newOCI, &newRun)
	require.NoError(t, err)

	// Verify update
	agents, err := service.ListAgents()
	require.NoError(t, err)
	assert.Len(t, agents, 1)
	assert.Equal(t, "test-agent", agents[0].Name)
	assert.Equal(t, "http://localhost:9081", agents[0].URL)
	assert.Equal(t, "docker.io/updated/agent:latest", agents[0].OCI)
	assert.True(t, agents[0].Run)

	// Try to update non-existing agent
	err = service.UpdateAgent("nonexistent", &newURL, nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAgentsConfigService_ListAgents(t *testing.T) {
	service := NewAgentsConfigService()
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	// Test with no agents
	agents, err := service.ListAgents()
	require.NoError(t, err)
	assert.Empty(t, agents)

	// Add agents
	err = service.AddAgent("agent1", "http://localhost:8081", "docker.io/agent1:latest", true)
	require.NoError(t, err)
	err = service.AddAgent("agent2", "http://localhost:8082", "", false)
	require.NoError(t, err)

	// List agents
	agents, err = service.ListAgents()
	require.NoError(t, err)
	assert.Len(t, agents, 2)

	assert.Equal(t, "agent1", agents[0].Name)
	assert.Equal(t, "http://localhost:8081", agents[0].URL)
	assert.Equal(t, "docker.io/agent1:latest", agents[0].OCI)
	assert.True(t, agents[0].Run)

	assert.Equal(t, "agent2", agents[1].Name)
	assert.Equal(t, "http://localhost:8082", agents[1].URL)
	assert.Empty(t, agents[1].OCI)
	assert.False(t, agents[1].Run)
}
