package domain

import (
	"context"
	"strings"
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

	// ReconcileAgents brings the supervised agents in line with agents.yaml:
	// agents no longer configured stop, new ones start, edited ones restart
	ReconcileAgents(ctx context.Context) (AgentChanges, error)

	// SetStatusCallback sets the callback function for agent status updates
	SetStatusCallback(callback func(agentName string, state AgentState, message string, url string, image string))

	// SetPullProgressCallback sets the callback function for image pull progress updates
	SetPullProgressCallback(callback func(agentName string, done, total int))
}

// AgentChanges is what reconciling the supervised agents with agents.yaml
// did: the agents that joined, left, or restarted with an edited entry.
type AgentChanges struct {
	Added     []string
	Removed   []string
	Restarted []string
}

// IsEmpty reports whether reconciling left every agent as it was.
func (c AgentChanges) IsEmpty() bool {
	return len(c.Added)+len(c.Removed)+len(c.Restarted) == 0
}

// String summarizes the changes as "agents +added -removed ~restarted".
func (c AgentChanges) String() string {
	parts := []string{"agents"}
	for _, group := range []struct {
		sign  string
		names []string
	}{{"+", c.Added}, {"-", c.Removed}, {"~", c.Restarted}} {
		for _, name := range group.names {
			parts = append(parts, group.sign+name)
		}
	}
	return strings.Join(parts, " ")
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
	AgentStateRemoved
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
	case AgentStateRemoved:
		return "Removed"
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
	case AgentStateRemoved:
		return "removed"
	default:
		return "unknown"
	}
}
