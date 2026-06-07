package states

import (
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// ExecutingToolsState handles events in the ExecutingTools state.
//
// This state processes tool execution completion:
//  1. ToolsCompletedEvent (Stop=false) → transitions to PostToolExecution
//  2. ToolsCompletedEvent (Stop=true)  → transitions to the Stopped terminal
//     (a rejected tool or a successful RequestPlanApproval ended the loop)
type ExecutingToolsState struct {
	ctx *domain.StateContext
}

// NewExecutingToolsState creates a new ExecutingTools state handler
func NewExecutingToolsState(ctx *domain.StateContext) domain.StateHandler {
	return &ExecutingToolsState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *ExecutingToolsState) Name() domain.AgentExecutionState {
	return domain.StateExecutingTools
}

// Handle processes events in ExecutingTools state
func (s *ExecutingToolsState) Handle(event domain.AgentEvent) error {
	switch e := event.(type) {
	case domain.ToolsCompletedEvent:
		if e.Stop {
			logger.Debug("tools execution signalled stop, terminating loop")
			if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateStopped); err != nil {
				logger.Error("failed to transition to stopped", "error", err)
				return err
			}
			return nil
		}

		logger.Debug("tools execution completed")
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StatePostToolExecution); err != nil {
			logger.Error("failed to transition to post tool execution", "error", err)
			return err
		}
		s.ctx.Events <- domain.MessageReceivedEvent{}
	}
	return nil
}
