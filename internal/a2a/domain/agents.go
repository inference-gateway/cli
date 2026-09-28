package domain

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

// AgentCardService resolves the configured A2A agents and their agent cards.
type AgentCardService interface {
	GetAgentCards(ctx context.Context) ([]*CachedAgentCard, error)
	GetConfiguredAgents() []string
}

// AgentSupervisor manages the lifecycle of A2A agent containers
type AgentSupervisor interface {
	// StartAgents starts all agents configured with run: true
	StartAgents(ctx context.Context) error

	// WaitForAgentsReady blocks until every run:true agent started by
	// StartAgents has settled (ready or failed), or ctx is done
	WaitForAgentsReady(ctx context.Context)

	// StopAgents stops all running agent containers
	StopAgents(ctx context.Context) error

	// StopAgent stops a specific agent container by name
	StopAgent(ctx context.Context, agentName string) error

	// IsRunning returns whether any agents are running
	IsRunning() bool

	// SetStatusCallback sets the callback function for agent status updates
	SetStatusCallback(callback func(agentName string, state AgentState, message string, url string, image string))

	// SetPullProgressCallback sets the callback function for image pull progress updates
	SetPullProgressCallback(callback func(agentName string, done, total int))
}

// AgentState represents the current state of an agent
type AgentState int

const (
	AgentStateUnknown AgentState = iota
	AgentStatePullingImage
	AgentStateStarting
	AgentStateWaitingReady
	AgentStateReady
	AgentStateFailed
)

func (a AgentState) String() string {
	switch a {
	case AgentStateUnknown:
		return "Unknown"
	case AgentStatePullingImage:
		return "PullingImage"
	case AgentStateStarting:
		return "Starting"
	case AgentStateWaitingReady:
		return "WaitingReady"
	case AgentStateReady:
		return "Ready"
	case AgentStateFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// DisplayName returns a user-friendly display name for the agent state
func (a AgentState) DisplayName() string {
	switch a {
	case AgentStateUnknown:
		return "unknown"
	case AgentStatePullingImage:
		return "pulling image"
	case AgentStateStarting:
		return "starting"
	case AgentStateWaitingReady:
		return "waiting"
	case AgentStateReady:
		return "ready"
	case AgentStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}
