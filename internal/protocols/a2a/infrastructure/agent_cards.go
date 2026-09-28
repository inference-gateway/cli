package infrastructure

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	adk "github.com/inference-gateway/adk/types"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
)

type AgentCardClient struct {
	config     *config.Config
	agentsPath string
	cache      map[string]*a2adomain.CachedAgentCard
	cacheMutex sync.RWMutex
}

func NewAgentCardClient(cfg *config.Config) *AgentCardClient {
	agentsPath := config.DefaultAgentsPath

	if homeDir, err := os.UserHomeDir(); err == nil {
		userspacePath := filepath.Join(homeDir, config.ConfigDirName, config.AgentsFileName)
		if _, err := os.Stat(userspacePath); err == nil {
			agentsPath = userspacePath
		}
	}

	if _, err := os.Stat(config.DefaultAgentsPath); err == nil {
		agentsPath = config.DefaultAgentsPath
	}

	return &AgentCardClient{
		config:     cfg,
		agentsPath: agentsPath,
		cache:      make(map[string]*a2adomain.CachedAgentCard),
	}
}

func (s *AgentCardClient) GetAgentCard(ctx context.Context, agentURL string) (*adk.AgentCard, error) {
	if s.config.A2A.Cache.Enabled {
		if card := s.getFromCache(agentURL); card != nil {
			return card, nil
		}
	}

	adkClient := NewClient(agentURL)
	card, err := adkClient.GetAgentCard(ctx)
	if err != nil {
		logger.Error("failed to fetch agent card", "agent_url", agentURL, "error", err)
		return nil, err
	}

	if s.config.A2A.Cache.Enabled {
		s.storeInCache(agentURL, card)
	}

	return card, nil
}

func (s *AgentCardClient) getFromCache(agentURL string) *adk.AgentCard {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()

	cachedCard, exists := s.cache[agentURL]
	if !exists {
		return nil
	}

	ttlDuration := time.Duration(s.config.A2A.Cache.TTL) * time.Second
	age := time.Since(cachedCard.FetchedAt)
	if age >= ttlDuration {
		return nil
	}

	return cachedCard.Card
}

func (s *AgentCardClient) storeInCache(agentURL string, card *adk.AgentCard) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	s.cache[agentURL] = &a2adomain.CachedAgentCard{
		Card:      card,
		URL:       agentURL,
		FetchedAt: time.Now(),
	}
}

func (s *AgentCardClient) GetConfiguredAgents() []string {
	if len(s.config.A2A.Agents) > 0 {
		return s.config.A2A.Agents
	}

	urls, err := config.GetAgentURLs(s.agentsPath)
	if err != nil {
		logger.Error("failed to load agents from agents.yaml", "error", err)
		return []string{}
	}

	return urls
}

func (s *AgentCardClient) GetAgentCards(ctx context.Context) ([]*a2adomain.CachedAgentCard, error) {
	agentURLs := s.GetConfiguredAgents()
	cards := make([]*a2adomain.CachedAgentCard, 0, len(agentURLs))

	for _, url := range agentURLs {
		card, err := s.GetAgentCard(ctx, url)
		if err != nil {
			logger.Error("failed to fetch agent card", "url", url, "error", err)
			continue
		}

		var cachedCard *a2adomain.CachedAgentCard

		if s.config.A2A.Cache.Enabled {
			s.cacheMutex.RLock()
			cachedCard = s.cache[url]
			s.cacheMutex.RUnlock()
		}

		if cachedCard == nil {
			cachedCard = &a2adomain.CachedAgentCard{
				Card:      card,
				URL:       url,
				FetchedAt: time.Now(),
			}
		}

		cards = append(cards, cachedCard)
	}

	return cards, nil
}
