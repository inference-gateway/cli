package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	require "github.com/stretchr/testify/require"
)

func TestAddAgent(t *testing.T) {
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

	if err := config.AddAgent(agentsPath, agent); err != nil {
		t.Fatalf("Failed to add agent: %v", err)
	}
	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		t.Fatal("Agents config file was not created")
	}

	cfg, err := config.LoadAgents(agentsPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	if len(cfg.Agents) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(cfg.Agents))
	}
	if cfg.Agents[0].Name != agent.Name {
		t.Errorf("Expected name %s, got %s", agent.Name, cfg.Agents[0].Name)
	}
	if cfg.Agents[0].URL != agent.URL {
		t.Errorf("Expected URL %s, got %s", agent.URL, cfg.Agents[0].URL)
	}
	if cfg.Agents[0].OCI != agent.OCI {
		t.Errorf("Expected OCI %s, got %s", agent.OCI, cfg.Agents[0].OCI)
	}
	if cfg.Agents[0].Run != agent.Run {
		t.Errorf("Expected Run %v, got %v", agent.Run, cfg.Agents[0].Run)
	}
	if len(cfg.Agents[0].Environment) != 2 {
		t.Errorf("Expected 2 environment variables, got %d", len(cfg.Agents[0].Environment))
	}
}

func TestAddAgent_Duplicate(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent := config.AgentEntry{Name: "test-agent", URL: "https://agent.example.com"}

	if err := config.AddAgent(agentsPath, agent); err != nil {
		t.Fatalf("Failed to add agent: %v", err)
	}
	if err := config.AddAgent(agentsPath, agent); err == nil {
		t.Fatal("Expected error when adding duplicate agent, got nil")
	}
}

func TestRemoveAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent1 := config.AgentEntry{Name: "agent1", URL: "https://agent1.example.com"}
	agent2 := config.AgentEntry{Name: "agent2", URL: "https://agent2.example.com"}

	require.NoError(t, config.AddAgent(agentsPath, agent1))
	require.NoError(t, config.AddAgent(agentsPath, agent2))

	if err := config.RemoveAgent(agentsPath, "agent1"); err != nil {
		t.Fatalf("Failed to remove agent: %v", err)
	}

	agents, err := config.ListAgents(agentsPath)
	if err != nil {
		t.Fatalf("Failed to list agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "agent2" {
		t.Errorf("Expected agent2 to remain, got %s", agents[0].Name)
	}
}

func TestRemoveAgent_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	if err := config.RemoveAgent(agentsPath, "nonexistent"); err == nil {
		t.Fatal("Expected error when removing nonexistent agent, got nil")
	}
}

func TestListAgents(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agents := []config.AgentEntry{
		{Name: "agent1", URL: "https://agent1.example.com"},
		{Name: "agent2", URL: "https://agent2.example.com"},
		{Name: "agent3", URL: "https://agent3.example.com"},
	}
	for _, agent := range agents {
		require.NoError(t, config.AddAgent(agentsPath, agent))
	}

	listed, err := config.ListAgents(agentsPath)
	if err != nil {
		t.Fatalf("Failed to list agents: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("Expected 3 agents, got %d", len(listed))
	}
}

func TestGetAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent := config.AgentEntry{Name: "test-agent", URL: "https://agent.example.com"}
	require.NoError(t, config.AddAgent(agentsPath, agent))

	retrieved, err := config.GetAgent(agentsPath, "test-agent")
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if retrieved.Name != agent.Name {
		t.Errorf("Expected name %s, got %s", agent.Name, retrieved.Name)
	}
	if retrieved.URL != agent.URL {
		t.Errorf("Expected URL %s, got %s", agent.URL, retrieved.URL)
	}
}

func TestGetAgent_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	if _, err := config.GetAgent(agentsPath, "nonexistent"); err == nil {
		t.Fatal("Expected error when getting nonexistent agent, got nil")
	}
}

func TestGetAgentURLs(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agents := []config.AgentEntry{
		{Name: "agent1", URL: "https://agent1.example.com"},
		{Name: "agent2", URL: "https://agent2.example.com"},
	}
	for _, agent := range agents {
		require.NoError(t, config.AddAgent(agentsPath, agent))
	}

	urls, err := config.GetAgentURLs(agentsPath)
	if err != nil {
		t.Fatalf("Failed to get agent URLs: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("Expected 2 URLs, got %d", len(urls))
	}

	expectedURLs := map[string]bool{
		"https://agent1.example.com": false,
		"https://agent2.example.com": false,
	}
	for _, url := range urls {
		if _, exists := expectedURLs[url]; !exists {
			t.Errorf("Unexpected URL: %s", url)
		}
		expectedURLs[url] = true
	}
	for url, found := range expectedURLs {
		if !found {
			t.Errorf("Expected URL not found: %s", url)
		}
	}
}

func TestLoadAgents_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "nonexistent.yaml")

	cfg, err := config.LoadAgents(agentsPath)
	if err != nil {
		t.Fatalf("Expected no error for nonexistent file, got: %v", err)
	}
	if len(cfg.Agents) != 0 {
		t.Errorf("Expected empty agents list, got %d agents", len(cfg.Agents))
	}
}

func TestUpdateAgent(t *testing.T) {
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
	require.NoError(t, config.AddAgent(agentsPath, agent))

	updatedAgent := config.AgentEntry{
		Name:  "test-agent",
		URL:   "https://new-agent.example.com",
		OCI:   "ghcr.io/org/test-agent:v2",
		Run:   true,
		Model: "anthropic/claude-4-5-sonnet",
		Environment: map[string]string{
			"DEBUG": "true",
		},
	}
	require.NoError(t, config.UpdateAgent(agentsPath, updatedAgent))

	retrieved, err := config.GetAgent(agentsPath, "test-agent")
	require.NoError(t, err)

	if retrieved.URL != updatedAgent.URL {
		t.Errorf("Expected URL %s, got %s", updatedAgent.URL, retrieved.URL)
	}
	if retrieved.OCI != updatedAgent.OCI {
		t.Errorf("Expected OCI %s, got %s", updatedAgent.OCI, retrieved.OCI)
	}
	if retrieved.Run != updatedAgent.Run {
		t.Errorf("Expected Run %v, got %v", updatedAgent.Run, retrieved.Run)
	}
	if retrieved.Model != updatedAgent.Model {
		t.Errorf("Expected Model %s, got %s", updatedAgent.Model, retrieved.Model)
	}
	if len(retrieved.Environment) != 1 || retrieved.Environment["DEBUG"] != "true" {
		t.Errorf("Expected Environment to be updated, got %v", retrieved.Environment)
	}

	agents, err := config.ListAgents(agentsPath)
	require.NoError(t, err)
	if len(agents) != 1 {
		t.Errorf("Expected 1 agent after update, got %d", len(agents))
	}
}

func TestUpdateAgent_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agent := config.AgentEntry{Name: "nonexistent", URL: "https://agent.example.com"}
	if err := config.UpdateAgent(agentsPath, agent); err == nil {
		t.Fatal("Expected error when updating nonexistent agent, got nil")
	}
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
