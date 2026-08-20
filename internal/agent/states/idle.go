package states

import (
	logger "github.com/inference-gateway/cli/internal/logger"
)

// IdleState handles events in the Idle state.
//
// The Idle state is the initial and final resting state of the agent.
// When a MessageReceivedEvent arrives, it transitions to CheckingQueue to begin processing.
type IdleState struct {
	ctx *StateContext
}

// NewIdleState creates a new Idle state handler
func NewIdleState(ctx *StateContext) StateHandler {
	return &IdleState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *IdleState) Name() AgentExecutionState {
	return StateIdle
}

// Handle processes events in Idle state
func (s *IdleState) Handle(event AgentEvent) error {
	switch event.(type) {
	case MessageReceivedEvent:
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCheckingQueue); err != nil {
			logger.Error("failed to transition to checking queue", "error", err)
			return err
		}
		logger.Debug("re-emitting message received event in checking queue state")
		s.ctx.Events <- event
	}
	return nil
}
