package infrastructure

import (
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	adk "github.com/inference-gateway/adk/types"

	config "github.com/inference-gateway/cli/config"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
)

func TestAgentCardClient_GetConfiguredAgents_EnvVarPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agentsCfg, err := config.LoadAgents(agentsPath)
	require.NoError(t, err)
	require.NoError(t, agentsCfg.CreateEntry(config.AgentEntry{
		Name: "yaml-agent",
		URL:  "http://yaml-agent:8080",
		Run:  false,
	}))

	t.Run("environment variable takes precedence", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.A2A.Agents = []string{
			"http://env-agent-1:8080",
			"http://env-agent-2:8080",
		}

		svc := &AgentCardClient{
			config:     cfg,
			agentsPath: agentsPath,
			cache:      make(map[string]*a2adomain.CachedAgentCard),
		}

		agents := svc.GetConfiguredAgents()

		require.Len(t, agents, 2)
		require.Equal(t, "http://env-agent-1:8080", agents[0])
		require.Equal(t, "http://env-agent-2:8080", agents[1])
	})

	t.Run("falls back to agents.yaml when env var empty", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.A2A.Agents = []string{}

		svc := &AgentCardClient{
			config:     cfg,
			agentsPath: agentsPath,
			cache:      make(map[string]*a2adomain.CachedAgentCard),
		}

		agents := svc.GetConfiguredAgents()

		require.Len(t, agents, 1)
		require.Equal(t, "http://yaml-agent:8080", agents[0])
	})

	t.Run("falls back to agents.yaml when env var nil", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.A2A.Agents = nil

		svc := &AgentCardClient{
			config:     cfg,
			agentsPath: agentsPath,
			cache:      make(map[string]*a2adomain.CachedAgentCard),
		}

		agents := svc.GetConfiguredAgents()

		require.Len(t, agents, 1)
		require.Equal(t, "http://yaml-agent:8080", agents[0])
	})
}

func TestAgentCardClient_GetConfiguredAgents_NoAgentsConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = nil

	svc := &AgentCardClient{
		config:     cfg,
		agentsPath: agentsPath,
		cache:      make(map[string]*a2adomain.CachedAgentCard),
	}

	agents := svc.GetConfiguredAgents()

	require.Len(t, agents, 0)
}

func TestPreferredEndpointURL(t *testing.T) {
	const fallback = "http://configured:8080"
	tests := []struct {
		name string
		card *adk.AgentCard
		want string
	}{
		{"no card", nil, fallback},
		{"no interfaces", &adk.AgentCard{}, fallback},
		{"first interface wins", &adk.AgentCard{SupportedInterfaces: []adk.AgentInterface{{URL: "http://first"}, {URL: "http://second"}}}, "http://first"},
		{"empty url skipped", &adk.AgentCard{SupportedInterfaces: []adk.AgentInterface{{URL: ""}, {URL: "http://second"}}}, "http://second"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, PreferredEndpointURL(tt.card, fallback))
		})
	}
}
