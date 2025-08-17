package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
)

// HTTPA2AService implements A2AService using HTTP API calls
type HTTPA2AService struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// A2AAgentsResponse represents the API response for listing A2A agents
type A2AAgentsResponse struct {
	Data []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Endpoint string `json:"endpoint,omitempty"`
		Version  string `json:"version,omitempty"`
	} `json:"data"`
}

// NewHTTPA2AService creates a new HTTP-based A2A service
func NewHTTPA2AService(baseURL, apiKey string) *HTTPA2AService {
	return &HTTPA2AService{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *HTTPA2AService) ListAgents(ctx context.Context) ([]domain.A2AAgent, error) {
	url := fmt.Sprintf("%s/v1/a2a/agents", s.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch A2A agents: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var agentsResp A2AAgentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&agentsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	agents := make([]domain.A2AAgent, len(agentsResp.Data))
	for i, agent := range agentsResp.Data {
		agents[i] = domain.A2AAgent{
			ID:       agent.ID,
			Name:     agent.Name,
			Status:   mapAgentStatus(agent.Status),
			Endpoint: agent.Endpoint,
			Version:  agent.Version,
		}
	}

	return agents, nil
}

// mapAgentStatus maps string status to domain status type
func mapAgentStatus(status string) domain.A2AAgentStatus {
	switch strings.ToLower(status) {
	case "available":
		return domain.A2AAgentStatusAvailable
	case "degraded":
		return domain.A2AAgentStatusDegraded
	default:
		return domain.A2AAgentStatusUnknown
	}
}
