package services

import (
	"context"
	"sync"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

type A2AAgentService struct {
	config     *config.Config
	cache      map[string]*domain.CachedAgentCard
	cacheMutex sync.RWMutex
}

func NewA2AAgentService(cfg *config.Config) *A2AAgentService {
	return &A2AAgentService{
		config: cfg,
		cache:  make(map[string]*domain.CachedAgentCard),
	}
}

func (s *A2AAgentService) GetAgentCard(ctx context.Context, agentURL string) (*adk.AgentCard, error) {
	if s.config.A2A.Cache.Enabled {
		if card := s.getFromCache(agentURL); card != nil {
			return card, nil
		}
	} else {
		logger.Debug("Cache is disabled, will fetch from server", "agent_url", agentURL)
	}

	logger.Debug("Fetching agent card from server", "agent_url", agentURL)
	adkClient := client.NewClient(agentURL)
	card, err := adkClient.GetAgentCard(ctx)
	if err != nil {
		logger.Error("Failed to fetch agent card", "agent_url", agentURL, "error", err)
		return nil, err
	}

	if s.config.A2A.Cache.Enabled {
		s.storeInCache(agentURL, card)
	}

	return card, nil
}

func (s *A2AAgentService) getFromCache(agentURL string) *adk.AgentCard {
	logger.Debug("Cache is enabled, checking for cached card", "agent_url", agentURL, "ttl", s.config.A2A.Cache.TTL)
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()

	cachedCard, exists := s.cache[agentURL]
	if !exists {
		logger.Debug("No cached card found", "agent_url", agentURL)
		return nil
	}

	ttlDuration := time.Duration(s.config.A2A.Cache.TTL) * time.Second
	age := time.Since(cachedCard.FetchedAt)
	if age >= ttlDuration {
		logger.Debug("Cached card expired", "agent_url", agentURL, "age_seconds", age.Seconds(), "ttl_seconds", ttlDuration.Seconds())
		return nil
	}

	logger.Debug("Returning cached agent card", "agent_url", agentURL, "age_seconds", age.Seconds(), "ttl_seconds", ttlDuration.Seconds())
	return cachedCard.Card
}

func (s *A2AAgentService) storeInCache(agentURL string, card *adk.AgentCard) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	s.cache[agentURL] = &domain.CachedAgentCard{
		Card:      card,
		URL:       agentURL,
		FetchedAt: time.Now(),
	}
	logger.Debug("Cached agent card", "agent_url", agentURL)
}

func (s *A2AAgentService) GetConfiguredAgents() []string {
	return s.config.A2A.Agents
}

func (s *A2AAgentService) GetAgentCards(ctx context.Context) ([]*domain.CachedAgentCard, error) {
	agentURLs := s.GetConfiguredAgents()
	cards := make([]*domain.CachedAgentCard, 0, len(agentURLs))

	for _, url := range agentURLs {
		card, err := s.GetAgentCard(ctx, url)
		if err != nil {
			logger.Error("Failed to fetch agent card", "url", url, "error", err)
			continue
		}

		var cachedCard *domain.CachedAgentCard

		if s.config.A2A.Cache.Enabled {
			s.cacheMutex.RLock()
			cachedCard = s.cache[url]
			s.cacheMutex.RUnlock()
		}

		if cachedCard == nil {
			cachedCard = &domain.CachedAgentCard{
				Card:      card,
				URL:       url,
				FetchedAt: time.Now(),
			}
		}

		cards = append(cards, cachedCard)
	}

	return cards, nil
}
