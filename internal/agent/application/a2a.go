// Package application holds agent-context orchestration contracts that touch
// external A2A (adk) types; they are deliberately outside the pure domain.
package application

import (
	"context"
	"time"

	adk "github.com/inference-gateway/adk/types"
)

// CachedAgentCard represents a cached agent card with metadata
type CachedAgentCard struct {
	Card      *adk.AgentCard `json:"card"`
	URL       string         `json:"url"`
	FetchedAt time.Time      `json:"fetched_at"`
}

// A2AAgentService manages A2A agent operations
type A2AAgentService interface {
	GetAgentCards(ctx context.Context) ([]*CachedAgentCard, error)
	GetConfiguredAgents() []string
}
