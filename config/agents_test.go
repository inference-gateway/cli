package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAgentsConfig(t *testing.T) {
	tempDir := t.TempDir()

	// Test with no config file
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	config, err := LoadAgentsConfig()
	require.NoError(t, err)
	assert.Empty(t, config.Agents)
}

func TestLoadAgentsConfigFromPath(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "agents.yaml")

	// Create test config
	testConfig := `
agents:
  - name: test-agent
    url: http://localhost:8081
    oci: docker.io/test/agent:latest
    run: true
  - name: another-agent
    url: http://localhost:8082
    run: false
`

	err := os.WriteFile(tempFile, []byte(testConfig), 0644)
	require.NoError(t, err)

	config, err := loadAgentsConfigFromPath(tempFile)
	require.NoError(t, err)

	assert.Len(t, config.Agents, 2)
	assert.Equal(t, "test-agent", config.Agents[0].Name)
	assert.Equal(t, "http://localhost:8081", config.Agents[0].URL)
	assert.Equal(t, "docker.io/test/agent:latest", config.Agents[0].OCI)
	assert.True(t, config.Agents[0].Run)

	assert.Equal(t, "another-agent", config.Agents[1].Name)
	assert.Equal(t, "http://localhost:8082", config.Agents[1].URL)
	assert.Empty(t, config.Agents[1].OCI)
	assert.False(t, config.Agents[1].Run)
}

func TestSaveAgentsConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, ".infer", "agents.yaml")

	config := &AgentsConfig{
		Agents: []A2AAgentConfig{
			{
				Name: "test-agent",
				URL:  "http://localhost:8081",
				OCI:  "docker.io/test/agent:latest",
				Run:  true,
			},
		},
	}

	err := SaveAgentsConfig(config, configPath)
	require.NoError(t, err)

	// Verify file was created
	assert.FileExists(t, configPath)

	// Load and verify content
	loadedConfig, err := loadAgentsConfigFromPath(configPath)
	require.NoError(t, err)

	assert.Len(t, loadedConfig.Agents, 1)
	assert.Equal(t, "test-agent", loadedConfig.Agents[0].Name)
	assert.Equal(t, "http://localhost:8081", loadedConfig.Agents[0].URL)
}

func TestAgentsConfig_GetAgentByName(t *testing.T) {
	config := &AgentsConfig{
		Agents: []A2AAgentConfig{
			{Name: "agent1", URL: "http://localhost:8081"},
			{Name: "agent2", URL: "http://localhost:8082"},
		},
	}

	// Test existing agent
	agent, err := config.GetAgentByName("agent1")
	require.NoError(t, err)
	assert.Equal(t, "agent1", agent.Name)
	assert.Equal(t, "http://localhost:8081", agent.URL)

	// Test non-existing agent
	_, err = config.GetAgentByName("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAgentsConfig_AddAgent(t *testing.T) {
	config := &AgentsConfig{}

	// Add first agent
	err := config.AddAgent(A2AAgentConfig{
		Name: "test-agent",
		URL:  "http://localhost:8081",
	})
	require.NoError(t, err)
	assert.Len(t, config.Agents, 1)

	// Try to add duplicate
	err = config.AddAgent(A2AAgentConfig{
		Name: "test-agent",
		URL:  "http://localhost:8082",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Len(t, config.Agents, 1)
}

func TestAgentsConfig_RemoveAgent(t *testing.T) {
	config := &AgentsConfig{
		Agents: []A2AAgentConfig{
			{Name: "agent1", URL: "http://localhost:8081"},
			{Name: "agent2", URL: "http://localhost:8082"},
		},
	}

	// Remove existing agent
	err := config.RemoveAgent("agent1")
	require.NoError(t, err)
	assert.Len(t, config.Agents, 1)
	assert.Equal(t, "agent2", config.Agents[0].Name)

	// Try to remove non-existing agent
	err = config.RemoveAgent("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAgentsConfig_UpdateAgent(t *testing.T) {
	config := &AgentsConfig{
		Agents: []A2AAgentConfig{
			{Name: "agent1", URL: "http://localhost:8081", Run: false},
		},
	}

	// Update existing agent
	err := config.UpdateAgent("agent1", A2AAgentConfig{
		Name: "agent1",
		URL:  "http://localhost:9081",
		Run:  true,
	})
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:9081", config.Agents[0].URL)
	assert.True(t, config.Agents[0].Run)

	// Try to update non-existing agent
	err = config.UpdateAgent("nonexistent", A2AAgentConfig{Name: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAgentsConfig_GetAgentURLs(t *testing.T) {
	config := &AgentsConfig{
		Agents: []A2AAgentConfig{
			{Name: "agent1", URL: "http://localhost:8081"},
			{Name: "agent2", URL: "http://localhost:8082"},
		},
	}

	urls := config.GetAgentURLs()
	expected := []string{"http://localhost:8081", "http://localhost:8082"}
	assert.Equal(t, expected, urls)
}
