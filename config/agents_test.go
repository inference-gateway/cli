package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	require "github.com/stretchr/testify/require"
)

func TestCreateEntry_Agent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent := config.AgentEntry{
		Name: "test-agent",
		URL:  "https://agent.example.com",
		OCI:  "ghcr.io/org/test-agent:latest",
		Run:  true,
		Environment: map[string]string{
			"API_KEY": "secret",
			"MODEL":   "gpt-4",
		},
	}

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.NoError(t, cfg.CreateEntry(agent))

	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		t.Fatal("Agents config file was not created")
	}

	reloaded, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.Len(t, reloaded.Agents, 1)
	require.Equal(t, agent.Name, reloaded.Agents[0].Name)
	require.Equal(t, agent.URL, reloaded.Agents[0].URL)
	require.Equal(t, agent.OCI, reloaded.Agents[0].OCI)
	require.Equal(t, agent.Run, reloaded.Agents[0].Run)
	require.Len(t, reloaded.Agents[0].Environment, 2)
}

func TestCreateEntry_Agent_Duplicate(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent := config.AgentEntry{Name: "test-agent", URL: "https://agent.example.com"}

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.NoError(t, cfg.CreateEntry(agent))
	require.Error(t, cfg.CreateEntry(agent), "expected duplicate-name error")
}

func TestDeleteEntry_Agent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent1 := config.AgentEntry{Name: "agent1", URL: "https://agent1.example.com"}
	agent2 := config.AgentEntry{Name: "agent2", URL: "https://agent2.example.com"}

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.NoError(t, cfg.CreateEntry(agent1))
	require.NoError(t, cfg.CreateEntry(agent2))
	require.NoError(t, cfg.DeleteEntry("agent1"))

	reloaded, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	agents := reloaded.ListEntries()
	require.Len(t, agents, 1)
	require.Equal(t, "agent2", agents[0].Name)
}

func TestDeleteEntry_Agent_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.Error(t, cfg.DeleteEntry("nonexistent"))
}

func TestListEntries_Agents(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	for _, agent := range []config.AgentEntry{
		{Name: "agent1", URL: "https://agent1.example.com"},
		{Name: "agent2", URL: "https://agent2.example.com"},
		{Name: "agent3", URL: "https://agent3.example.com"},
	} {
		require.NoError(t, cfg.CreateEntry(agent))
	}

	reloaded, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.Len(t, reloaded.ListEntries(), 3)
}

func TestReadEntry_Agent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent := config.AgentEntry{Name: "test-agent", URL: "https://agent.example.com"}

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.NoError(t, cfg.CreateEntry(agent))

	retrieved, err := cfg.ReadEntry("test-agent")
	require.NoError(t, err)
	require.Equal(t, agent.Name, retrieved.Name)
	require.Equal(t, agent.URL, retrieved.URL)
}

func TestReadEntry_Agent_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	_, err = cfg.ReadEntry("nonexistent")
	require.Error(t, err)
}

func TestGetAgentURLs(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	for _, agent := range []config.AgentEntry{
		{Name: "agent1", URL: "https://agent1.example.com"},
		{Name: "agent2", URL: "https://agent2.example.com"},
	} {
		require.NoError(t, cfg.CreateEntry(agent))
	}

	urls, err := config.GetAgentURLs(agentsPath)
	require.NoError(t, err)
	require.Len(t, urls, 2)

	expectedURLs := map[string]bool{
		"https://agent1.example.com": false,
		"https://agent2.example.com": false,
	}
	for _, url := range urls {
		_, exists := expectedURLs[url]
		require.True(t, exists, "unexpected URL: %s", url)
		expectedURLs[url] = true
	}
	for url, found := range expectedURLs {
		require.True(t, found, "expected URL not found: %s", url)
	}
}

func TestLoadAgents_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "nonexistent.yaml")

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.Empty(t, cfg.Agents)
}

func TestUpdateEntry_Agent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent := config.AgentEntry{
		Name:  "test-agent",
		URL:   "https://agent.example.com",
		OCI:   "ghcr.io/org/test-agent:v1",
		Run:   false,
		Model: "openai/gpt-4",
		Environment: map[string]string{
			"API_KEY": "secret",
		},
	}
	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.NoError(t, cfg.CreateEntry(agent))

	updated := config.AgentEntry{
		Name:  "test-agent",
		URL:   "https://new-agent.example.com",
		OCI:   "ghcr.io/org/test-agent:v2",
		Run:   true,
		Model: "anthropic/claude-4-5-sonnet",
		Environment: map[string]string{
			"DEBUG": "true",
		},
	}
	require.NoError(t, cfg.UpdateEntry(updated))

	reloaded, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	retrieved, err := reloaded.ReadEntry("test-agent")
	require.NoError(t, err)

	require.Equal(t, updated.URL, retrieved.URL)
	require.Equal(t, updated.OCI, retrieved.OCI)
	require.Equal(t, updated.Run, retrieved.Run)
	require.Equal(t, updated.Model, retrieved.Model)
	require.Len(t, retrieved.Environment, 1)
	require.Equal(t, "true", retrieved.Environment["DEBUG"])
	require.Len(t, reloaded.ListEntries(), 1)
}

func TestUpdateEntry_Agent_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent := config.AgentEntry{Name: "nonexistent", URL: "https://agent.example.com"}
	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.Error(t, cfg.UpdateEntry(agent))
}

func TestLoadAgents_EnvironmentVariableExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	t.Setenv("TEST_API_KEY", "secret-key-123")
	t.Setenv("TEST_MODEL", "gpt-4")
	t.Setenv("TEST_DEBUG", "true")

	configContent := `agents:
  - name: test-agent
    url: http://localhost:8080
    oci: ghcr.io/org/test-agent:latest
    run: true
    model: openai/$TEST_MODEL
    environment:
      API_KEY: $TEST_API_KEY
      DEBUG: ${TEST_DEBUG}
      STATIC_VAR: static-value
`
	require.NoError(t, os.WriteFile(agentsPath, []byte(configContent), 0644))

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)

	require.Len(t, cfg.Agents, 1, "Expected 1 agent")
	agent := cfg.Agents[0]
	require.Equal(t, "test-agent", agent.Name)
	require.Equal(t, "openai/gpt-4", agent.Model)

	require.Len(t, agent.Environment, 3, "Expected 3 environment variables")
	require.Equal(t, "secret-key-123", agent.Environment["API_KEY"], "API_KEY should be expanded")
	require.Equal(t, "true", agent.Environment["DEBUG"], "DEBUG should be expanded")
	require.Equal(t, "static-value", agent.Environment["STATIC_VAR"], "STATIC_VAR should remain unchanged")
}

func TestLoadAgents_EnvironmentVariableExpansion_UndefinedVar(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	configContent := `agents:
  - name: test-agent
    url: http://localhost:8080
    oci: ghcr.io/org/test-agent:latest
    run: true
    environment:
      UNDEFINED: $UNDEFINED_VAR
`
	require.NoError(t, os.WriteFile(agentsPath, []byte(configContent), 0644))

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)

	require.Len(t, cfg.Agents, 1)
	require.Equal(t, "", cfg.Agents[0].Environment["UNDEFINED"])
}

func TestLoadAgents_EnvironmentVariableExpansion_MixedSyntax(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	t.Setenv("VAR1", "value1")
	t.Setenv("VAR2", "value2")

	configContent := `agents:
  - name: test-agent
    url: http://localhost:8080
    oci: ghcr.io/org/test-agent:latest
    run: true
    environment:
      SYNTAX1: $VAR1
      SYNTAX2: ${VAR2}
      COMBINED: prefix-$VAR1-${VAR2}-suffix
`
	require.NoError(t, os.WriteFile(agentsPath, []byte(configContent), 0644))

	cfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)

	agent := cfg.Agents[0]
	require.Equal(t, "value1", agent.Environment["SYNTAX1"])
	require.Equal(t, "value2", agent.Environment["SYNTAX2"])
	require.Equal(t, "prefix-value1-value2-suffix", agent.Environment["COMBINED"])
}
