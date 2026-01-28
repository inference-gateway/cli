package states

import (
	domain "github.com/inference-gateway/cli/internal/domain"
)

// ErrorState handles events in the Error state.
//
// The Error state is a terminal state reached when unrecoverable errors occur.
// The event loop will exit when this state is reached.
type ErrorState struct {
	ctx *domain.StateContext
}

// NewErrorState creates a new Error state handler
func NewErrorState(ctx *domain.StateContext) domain.StateHandler {
	return &ErrorState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *ErrorState) Name() domain.AgentExecutionState {
	return domain.StateError
}

// Handle processes events in Error state
// This is a terminal state, so no events are expected
func (s *ErrorState) Handle(event domain.AgentEvent) error {
	// Terminal state - no events to handle
	return nil
}
