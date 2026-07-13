package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	require "github.com/stretchr/testify/require"
)

func TestCreateEntry_Agent(t *testing.T) {
	tests := []struct {
		name      string
		agent     config.AgentEntry
		seedFirst bool // whether to create the entry once before the test call
		wantErr   bool
	}{
		{
			name: "success",
			agent: config.AgentEntry{
				Name: "test-agent",
				URL:  "https://agent.example.com",
				OCI:  "ghcr.io/org/test-agent:latest",
				Run:  true,
				Environment: map[string]string{
					"API_KEY": "secret",
					"MODEL":   "gpt-4",
				},
			},
			seedFirst: false,
			wantErr:   false,
		},
		{
			name:      "duplicate",
			agent:     config.AgentEntry{Name: "test-agent", URL: "https://agent.example.com"},
			seedFirst: true,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			agentsPath := filepath.Join(tmpDir, "agents.yaml")

			cfg, err := config.LoadAgents(agentsPath)
			require.NoError(t, err)

			if tt.seedFirst {
				require.NoError(t, cfg.CreateEntry(tt.agent))
			}

			err = cfg.CreateEntry(tt.agent)
			if tt.wantErr {
				require.Error(t, err, "expected duplicate-name error")
				return
			}
			require.NoError(t, err)

			if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
				t.Fatal("Agents config file was not created")
			}

			reloaded, err := config.LoadAgents(agentsPath)
			require.NoError(t, err)
			require.Len(t, reloaded.Agents, 1)
			require.Equal(t, tt.agent.Name, reloaded.Agents[0].Name)
			require.Equal(t, tt.agent.URL, reloaded.Agents[0].URL)
			require.Equal(t, tt.agent.OCI, reloaded.Agents[0].OCI)
			require.Equal(t, tt.agent.Run, reloaded.Agents[0].Run)
			require.Len(t, reloaded.Agents[0].Environment, 2)
		})
	}
}

func TestDeleteEntry_Agent(t *testing.T) {
	tests := []struct {
		name      string
		seedNames []string
		deleteKey string
		wantErr   bool
		wantLen   int
		wantFirst string
	}{
		{
			name:      "success",
			seedNames: []string{"agent1", "agent2"},
			deleteKey: "agent1",
			wantErr:   false,
			wantLen:   1,
			wantFirst: "agent2",
		},
		{
			name:      "nonexistent",
			seedNames: nil,
			deleteKey: "nonexistent",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			agentsPath := filepath.Join(tmpDir, "agents.yaml")

			cfg, err := config.LoadAgents(agentsPath)
			require.NoError(t, err)

			for _, name := range tt.seedNames {
				require.NoError(t, cfg.CreateEntry(config.AgentEntry{Name: name, URL: "https://" + name + ".example.com"}))
			}

			err = cfg.DeleteEntry(tt.deleteKey)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			reloaded, err := config.LoadAgents(agentsPath)
			require.NoError(t, err)
			agents := reloaded.ListEntries()
			require.Len(t, agents, tt.wantLen)
			require.Equal(t, tt.wantFirst, agents[0].Name)
		})
	}
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
	tests := []struct {
		name     string
		seedName string
		readKey  string
		wantErr  bool
		wantURL  string
	}{
		{
			name:     "success",
			seedName: "test-agent",
			readKey:  "test-agent",
			wantURL:  "https://agent.example.com",
		},
		{
			name:    "nonexistent",
			readKey: "nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			agentsPath := filepath.Join(tmpDir, "agents.yaml")

			cfg, err := config.LoadAgents(agentsPath)
			require.NoError(t, err)

			if tt.seedName != "" {
				require.NoError(t, cfg.CreateEntry(config.AgentEntry{Name: tt.seedName, URL: tt.wantURL}))
			}

			retrieved, err := cfg.ReadEntry(tt.readKey)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.seedName, retrieved.Name)
			require.Equal(t, tt.wantURL, retrieved.URL)
		})
	}
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
	tests := []struct {
		name    string
		seed    *config.AgentEntry
		update  config.AgentEntry
		wantErr bool
	}{
		{
			name: "success",
			seed: &config.AgentEntry{
				Name: "test-agent", URL: "https://agent.example.com",
				OCI: "ghcr.io/org/test-agent:v1", Run: false,
				Model:       "openai/gpt-4",
				Environment: map[string]string{"API_KEY": "secret"},
			},
			update: config.AgentEntry{
				Name: "test-agent", URL: "https://new-agent.example.com",
				OCI: "ghcr.io/org/test-agent:v2", Run: true,
				Model:       "anthropic/claude-sonnet-4-6",
				Environment: map[string]string{"DEBUG": "true"},
			},
		},
		{
			name:    "nonexistent",
			update:  config.AgentEntry{Name: "nonexistent", URL: "https://agent.example.com"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			agentsPath := filepath.Join(tmpDir, "agents.yaml")

			cfg, err := config.LoadAgents(agentsPath)
			require.NoError(t, err)

			if tt.seed != nil {
				require.NoError(t, cfg.CreateEntry(*tt.seed))
			}

			err = cfg.UpdateEntry(tt.update)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			reloaded, err := config.LoadAgents(agentsPath)
			require.NoError(t, err)
			retrieved, err := reloaded.ReadEntry("test-agent")
			require.NoError(t, err)

			require.Equal(t, tt.update.URL, retrieved.URL)
			require.Equal(t, tt.update.OCI, retrieved.OCI)
			require.Equal(t, tt.update.Run, retrieved.Run)
			require.Equal(t, tt.update.Model, retrieved.Model)
			require.Len(t, retrieved.Environment, 1)
			require.Equal(t, "true", retrieved.Environment["DEBUG"])
			require.Len(t, reloaded.ListEntries(), 1)
		})
	}
}

func TestLoadAgents_EnvironmentVariableExpansion(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		yaml    string
		check   func(t *testing.T, cfg *config.AgentsConfig)
	}{
		{
			name: "standard expansion",
			envVars: map[string]string{
				"TEST_API_KEY": "secret-key-123",
				"TEST_MODEL":   "gpt-4",
				"TEST_DEBUG":   "true",
			},
			yaml: `agents:
  - name: test-agent
    url: http://localhost:8080
    oci: ghcr.io/org/test-agent:latest
    run: true
    model: openai/$TEST_MODEL
    environment:
      API_KEY: $TEST_API_KEY
      DEBUG: ${TEST_DEBUG}
      STATIC_VAR: static-value
`,
			check: func(t *testing.T, cfg *config.AgentsConfig) {
				require.Len(t, cfg.Agents, 1, "Expected 1 agent")
				agent := cfg.Agents[0]
				require.Equal(t, "test-agent", agent.Name)
				require.Equal(t, "openai/gpt-4", agent.Model)
				require.Len(t, agent.Environment, 3, "Expected 3 environment variables")
				require.Equal(t, "secret-key-123", agent.Environment["API_KEY"], "API_KEY should be expanded")
				require.Equal(t, "true", agent.Environment["DEBUG"], "DEBUG should be expanded")
				require.Equal(t, "static-value", agent.Environment["STATIC_VAR"], "STATIC_VAR should remain unchanged")
			},
		},
		{
			name: "undefined var",
			yaml: `agents:
  - name: test-agent
    url: http://localhost:8080
    oci: ghcr.io/org/test-agent:latest
    run: true
    environment:
      UNDEFINED: $UNDEFINED_VAR
`,
			check: func(t *testing.T, cfg *config.AgentsConfig) {
				require.Len(t, cfg.Agents, 1)
				require.Equal(t, "", cfg.Agents[0].Environment["UNDEFINED"])
			},
		},
		{
			name: "mixed syntax",
			envVars: map[string]string{
				"VAR1": "value1",
				"VAR2": "value2",
			},
			yaml: `agents:
  - name: test-agent
    url: http://localhost:8080
    oci: ghcr.io/org/test-agent:latest
    run: true
    environment:
      SYNTAX1: $VAR1
      SYNTAX2: ${VAR2}
      COMBINED: prefix-$VAR1-${VAR2}-suffix
`,
			check: func(t *testing.T, cfg *config.AgentsConfig) {
				agent := cfg.Agents[0]
				require.Equal(t, "value1", agent.Environment["SYNTAX1"])
				require.Equal(t, "value2", agent.Environment["SYNTAX2"])
				require.Equal(t, "prefix-value1-value2-suffix", agent.Environment["COMBINED"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			agentsPath := filepath.Join(tmpDir, "agents.yaml")

			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			require.NoError(t, os.WriteFile(agentsPath, []byte(tt.yaml), 0644))

			cfg, err := config.LoadAgents(agentsPath)
			require.NoError(t, err)

			tt.check(t, cfg)
		})
	}
}
