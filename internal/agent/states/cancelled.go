package states

import (
	domain "github.com/inference-gateway/cli/internal/domain"
)

// CancelledState handles events in the Cancelled state.
//
// The Cancelled state is a terminal state reached when the agent is cancelled by the user.
// The event loop will exit when this state is reached.
type CancelledState struct {
	ctx *domain.StateContext
}

// NewCancelledState creates a new Cancelled state handler
func NewCancelledState(ctx *domain.StateContext) domain.StateHandler {
	return &CancelledState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *CancelledState) Name() domain.AgentExecutionState {
	return domain.StateCancelled
}

// Handle processes events in Cancelled state
// This is a terminal state, so no events are expected
func (s *CancelledState) Handle(event domain.AgentEvent) error {
	// Terminal state - no events to handle
	return nil
}
