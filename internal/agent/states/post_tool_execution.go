package states

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// PostToolExecutionState handles events in the PostToolExecution state.
//
// This state:
//  1. Checks if messages were queued during tool execution → drains and goes to CheckingQueue
//  2. Checks if can complete → Completing
//  3. Otherwise → CheckingQueue for next turn
type PostToolExecutionState struct {
	ctx *StateContext
}

// NewPostToolExecutionState creates a new PostToolExecution state handler
func NewPostToolExecutionState(ctx *StateContext) StateHandler {
	return &PostToolExecutionState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *PostToolExecutionState) Name() AgentExecutionState {
	return StatePostToolExecution
}

// Handle processes events in PostToolExecution state
func (s *PostToolExecutionState) Handle(event AgentEvent) error {
	logger.Debug("post tool execution state: evaluating next action",
		"turn", s.ctx.AgentCtx.Turns,
		"max_turns", s.ctx.AgentCtx.MaxTurns,
		"queue_empty", s.ctx.AgentCtx.MessageQueue.IsEmpty())

	if s.ctx.DispatchHooks != nil {
		s.ctx.DispatchHooks(agentdomain.HookPostTool)
	}

	if !s.ctx.AgentCtx.MessageQueue.IsEmpty() {
		return s.handleQueuedMessages()
	}

	if s.ctx.StateMachine.CanTransition(s.ctx.AgentCtx, StateCompleting) {
		logger.Debug("can complete after tool execution")
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCompleting); err != nil {
			logger.Error("failed to transition to completing", "error", err)
			return err
		}
		s.ctx.Events <- CompletionRequestedEvent{}
	} else {
		logger.Debug("continuing to next turn", "current_turn", s.ctx.AgentCtx.Turns, "max", s.ctx.AgentCtx.MaxTurns)
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCheckingQueue); err != nil {
			logger.Error("failed to transition to checking queue", "error", err)
			return err
		}
		s.ctx.Events <- MessageReceivedEvent{}
	}
	return nil
}

// handleQueuedMessages drains queued messages into conversation history. If
// the session ctx was cancelled (Esc), it short-circuits to Completing so
// the queued input is preserved without starting another LLM turn -
// matching the "drain then stop" contract from issue #532.
func (s *PostToolExecutionState) handleQueuedMessages() error {
	logger.Debug("messages queued during tool execution, draining queue")
	if s.ctx.DispatchHooks != nil {
		s.ctx.DispatchHooks(agentdomain.HookPreQueueDrain)
	}
	numBatched := s.ctx.BatchDrainQueue()
	logger.Debug("batched messages after tool execution", "count", numBatched)
	if numBatched > 0 && s.ctx.DispatchHooks != nil {
		s.ctx.DispatchHooks(agentdomain.HookPostQueueDrain)
	}

	if s.ctx.AgentCtx.Ctx.Err() != nil {
		logger.Debug("session cancelled after drain - completing without next turn",
			"err", s.ctx.AgentCtx.Ctx.Err())
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCompleting); err != nil {
			logger.Error("failed to transition to completing", "error", err)
			return err
		}
		s.ctx.Events <- CompletionRequestedEvent{}
		return nil
	}

	if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCheckingQueue); err != nil {
		logger.Error("failed to transition to checking queue", "error", err)
		return err
	}
	s.ctx.Events <- MessageReceivedEvent{}
	return nil
}
