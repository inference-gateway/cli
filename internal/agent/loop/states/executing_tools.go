package states

import (
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// ExecutingToolsState handles events in the ExecutingTools state.
//
// This state processes tool execution completion:
//  1. ToolsCompletedEvent → transitions to PostToolExecution
type ExecutingToolsState struct {
	ctx *StateContext
}

// NewExecutingToolsState creates a new ExecutingTools state handler
func NewExecutingToolsState(ctx *StateContext) StateHandler {
	return &ExecutingToolsState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *ExecutingToolsState) Name() AgentExecutionState {
	return StateExecutingTools
}

// Handle processes events in ExecutingTools state
func (s *ExecutingToolsState) Handle(event AgentEvent) error {
	switch event.(type) {
	case ToolsCompletedEvent:
		logger.Debug("tools execution completed")
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StatePostToolExecution); err != nil {
			logger.Error("failed to transition to post tool execution", "error", err)
			return err
		}
		s.ctx.Events <- MessageReceivedEvent{}
	}
	return nil
}
