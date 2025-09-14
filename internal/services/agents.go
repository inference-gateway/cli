package services

import (
	"context"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

type A2AAgentService struct {
	config *config.Config
}

func NewA2AAgentService(cfg *config.Config) *A2AAgentService {
	return &A2AAgentService{
		config: cfg,
	}
}

func (s *A2AAgentService) GetAgentCard(ctx context.Context, agentURL string) (*adk.AgentCard, error) {
	logger.Debug("Fetching agent card from server", "agent_url", agentURL)
	adkClient := client.NewClient(agentURL)
	card, err := adkClient.GetAgentCard(ctx)
	if err != nil {
		logger.Error("Failed to fetch agent card", "agent_url", agentURL, "error", err)
		return nil, err
	}
	return card, nil
}

func (s *A2AAgentService) GetConfiguredAgents() []string {
	return s.config.A2A.Agents
}

func (s *A2AAgentService) GetAllAgentCards(ctx context.Context) ([]*domain.CachedAgentCard, error) {
	agentURLs := s.GetConfiguredAgents()
	cards := make([]*domain.CachedAgentCard, 0, len(agentURLs))

	for _, url := range agentURLs {
		card, err := s.GetAgentCard(ctx, url)
		if err != nil {
			logger.Error("Failed to fetch agent card", "url", url, "error", err)
			continue
		}

		cachedCard := &domain.CachedAgentCard{
			Card: card,
			URL:  url,
		}
		cards = append(cards, cachedCard)
	}

	return cards, nil
}

func (s *A2AAgentService) GetSystemPromptAgentInfo(ctx context.Context) string {
	agentURLs := s.GetConfiguredAgents()
	logger.Debug("GetSystemPromptAgentInfo called", "agent_count", len(agentURLs), "urls", agentURLs)

	if len(agentURLs) == 0 {
		logger.Debug("No A2A agents configured, returning empty string")
		return ""
	}

	var agentInfo string
	for _, url := range agentURLs {
		card, err := s.GetAgentCard(ctx, url)
		if err != nil {
			logger.Error("Failed to get agent card for system prompt", "url", url, "error", err)
			continue
		}

		agentInfo += "- " + card.Name + ": " + card.Description + " (URL: " + url + ")\n"
		logger.Debug("Added agent to system prompt", "name", card.Name, "url", url)
	}

	if agentInfo != "" {
		result := "\n\nAvailable A2A Agents:\n" + agentInfo
		logger.Debug("Generated A2A agent info for system prompt", "length", len(result))
		return result
	}
	logger.Debug("No agent info generated for system prompt")
	return ""
}
