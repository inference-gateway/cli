package services

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	require "github.com/stretchr/testify/require"
)

func TestAgentsConfigService_AddAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

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

	err := svc.AddAgent(agent)
	if err != nil {
		t.Fatalf("Failed to add agent: %v", err)
	}

	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		t.Fatal("Agents config file was not created")
	}

	cfg, err := svc.Load()
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

func TestAgentsConfigService_AddDuplicateAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

	agent := config.AgentEntry{
		Name: "test-agent",
		URL:  "https://agent.example.com",
	}

	err := svc.AddAgent(agent)
	if err != nil {
		t.Fatalf("Failed to add agent: %v", err)
	}

	err = svc.AddAgent(agent)
	if err == nil {
		t.Fatal("Expected error when adding duplicate agent, got nil")
	}
}

func TestAgentsConfigService_RemoveAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

	agent1 := config.AgentEntry{
		Name: "agent1",
		URL:  "https://agent1.example.com",
	}

	agent2 := config.AgentEntry{
		Name: "agent2",
		URL:  "https://agent2.example.com",
	}

	require.NoError(t, svc.AddAgent(agent1))
	require.NoError(t, svc.AddAgent(agent2))

	err := svc.RemoveAgent("agent1")
	if err != nil {
		t.Fatalf("Failed to remove agent: %v", err)
	}

	agents, err := svc.ListAgents()
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

func TestAgentsConfigService_RemoveNonexistentAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

	err := svc.RemoveAgent("nonexistent")
	if err == nil {
		t.Fatal("Expected error when removing nonexistent agent, got nil")
	}
}

func TestAgentsConfigService_ListAgents(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

	agents := []config.AgentEntry{
		{Name: "agent1", URL: "https://agent1.example.com"},
		{Name: "agent2", URL: "https://agent2.example.com"},
		{Name: "agent3", URL: "https://agent3.example.com"},
	}

	for _, agent := range agents {
		require.NoError(t, svc.AddAgent(agent))
	}

	listed, err := svc.ListAgents()
	if err != nil {
		t.Fatalf("Failed to list agents: %v", err)
	}

	if len(listed) != 3 {
		t.Fatalf("Expected 3 agents, got %d", len(listed))
	}
}

func TestAgentsConfigService_GetAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

	agent := config.AgentEntry{
		Name: "test-agent",
		URL:  "https://agent.example.com",
	}

	require.NoError(t, svc.AddAgent(agent))

	retrieved, err := svc.GetAgent("test-agent")
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

func TestAgentsConfigService_GetNonexistentAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

	_, err := svc.GetAgent("nonexistent")
	if err == nil {
		t.Fatal("Expected error when getting nonexistent agent, got nil")
	}
}

func TestAgentsConfigService_GetAgentURLs(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

	agents := []config.AgentEntry{
		{Name: "agent1", URL: "https://agent1.example.com"},
		{Name: "agent2", URL: "https://agent2.example.com"},
	}

	for _, agent := range agents {
		require.NoError(t, svc.AddAgent(agent))
	}

	urls, err := svc.GetAgentURLs()
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

func TestAgentsConfigService_LoadNonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "nonexistent.yaml")
	svc := NewAgentsConfigService(agentsPath)

	cfg, err := svc.Load()
	if err != nil {
		t.Fatalf("Expected no error for nonexistent file, got: %v", err)
	}

	if len(cfg.Agents) != 0 {
		t.Errorf("Expected empty agents list, got %d agents", len(cfg.Agents))
	}
}

func TestAgentsConfigService_UpdateAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

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

	require.NoError(t, svc.AddAgent(agent))

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

	err := svc.UpdateAgent(updatedAgent)
	require.NoError(t, err)

	retrieved, err := svc.GetAgent("test-agent")
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

	agents, err := svc.ListAgents()
	require.NoError(t, err)

	if len(agents) != 1 {
		t.Errorf("Expected 1 agent after update, got %d", len(agents))
	}
}

func TestAgentsConfigService_UpdateNonexistentAgent(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")
	svc := NewAgentsConfigService(agentsPath)

	agent := config.AgentEntry{
		Name: "nonexistent",
		URL:  "https://agent.example.com",
	}

	err := svc.UpdateAgent(agent)
	if err == nil {
		t.Fatal("Expected error when updating nonexistent agent, got nil")
	}
}

func TestAgentsConfigService_EnvironmentVariableExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	// Set test environment variables
	t.Setenv("TEST_API_KEY", "secret-key-123")
	t.Setenv("TEST_MODEL", "gpt-4")
	t.Setenv("TEST_DEBUG", "true")

	// Write config file with env var placeholders
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

	svc := NewAgentsConfigService(agentsPath)
	cfg, err := svc.Load()
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

func TestAgentsConfigService_EnvironmentVariableExpansion_UndefinedVar(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	// Write config file with undefined env var
	configContent := `agents:
  - name: test-agent
    url: http://localhost:8080
    oci: ghcr.io/org/test-agent:latest
    run: true
    environment:
      UNDEFINED: $UNDEFINED_VAR
`
	require.NoError(t, os.WriteFile(agentsPath, []byte(configContent), 0644))

	svc := NewAgentsConfigService(agentsPath)
	cfg, err := svc.Load()
	require.NoError(t, err)

	require.Len(t, cfg.Agents, 1)
	// Undefined env vars expand to empty string
	require.Equal(t, "", cfg.Agents[0].Environment["UNDEFINED"])
}

func TestAgentsConfigService_EnvironmentVariableExpansion_MixedSyntax(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	t.Setenv("VAR1", "value1")
	t.Setenv("VAR2", "value2")

	// Test both $VAR and ${VAR} syntax
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

	svc := NewAgentsConfigService(agentsPath)
	cfg, err := svc.Load()
	require.NoError(t, err)

	agent := cfg.Agents[0]
	require.Equal(t, "value1", agent.Environment["SYNTAX1"])
	require.Equal(t, "value2", agent.Environment["SYNTAX2"])
	require.Equal(t, "prefix-value1-value2-suffix", agent.Environment["COMBINED"])
}
