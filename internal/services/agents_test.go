package services

import (
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	require "github.com/stretchr/testify/require"
)

func TestA2AAgentService_GetConfiguredAgents_EnvVarPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agentsConfigSvc := NewAgentsConfigService(agentsPath)
	err := agentsConfigSvc.AddAgent(config.AgentEntry{
		Name: "yaml-agent",
		URL:  "http://yaml-agent:8080",
		Run:  false,
	})
	require.NoError(t, err)

	t.Run("environment variable takes precedence", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.A2A.Agents = []string{
			"http://env-agent-1:8080",
			"http://env-agent-2:8080",
		}

		svc := &A2AAgentService{
			config:          cfg,
			agentsConfigSvc: agentsConfigSvc,
			cache:           make(map[string]*domain.CachedAgentCard),
		}

		agents := svc.GetConfiguredAgents()

		require.Len(t, agents, 2)
		require.Equal(t, "http://env-agent-1:8080", agents[0])
		require.Equal(t, "http://env-agent-2:8080", agents[1])
	})

	t.Run("falls back to agents.yaml when env var empty", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.A2A.Agents = []string{}

		svc := &A2AAgentService{
			config:          cfg,
			agentsConfigSvc: agentsConfigSvc,
			cache:           make(map[string]*domain.CachedAgentCard),
		}

		agents := svc.GetConfiguredAgents()

		require.Len(t, agents, 1)
		require.Equal(t, "http://yaml-agent:8080", agents[0])
	})

	t.Run("falls back to agents.yaml when env var nil", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.A2A.Agents = nil

		svc := &A2AAgentService{
			config:          cfg,
			agentsConfigSvc: agentsConfigSvc,
			cache:           make(map[string]*domain.CachedAgentCard),
		}

		agents := svc.GetConfiguredAgents()

		require.Len(t, agents, 1)
		require.Equal(t, "http://yaml-agent:8080", agents[0])
	})
}

func TestA2AAgentService_GetConfiguredAgents_NoAgentsConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	agentsPath := filepath.Join(tmpDir, "agents.yaml")

	agentsConfigSvc := NewAgentsConfigService(agentsPath)

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = nil

	svc := &A2AAgentService{
		config:          cfg,
		agentsConfigSvc: agentsConfigSvc,
		cache:           make(map[string]*domain.CachedAgentCard),
	}

	agents := svc.GetConfiguredAgents()

	require.Len(t, agents, 0)
}
