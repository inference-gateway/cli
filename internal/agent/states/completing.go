package states

import (
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// CompletingState handles events in the Completing state.
//
// This state finalizes the agent execution:
//  1. Performs a final 100ms queue check
//  2. If messages queued → restart agent (CheckingQueue)
//  3. Otherwise → publish completion event and transition to Idle
type CompletingState struct {
	ctx *domain.StateContext
}

// NewCompletingState creates a new Completing state handler
func NewCompletingState(ctx *domain.StateContext) domain.StateHandler {
	return &CompletingState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *CompletingState) Name() domain.AgentExecutionState {
	return domain.StateCompleting
}

// Handle processes events in Completing state. Completion is driven solely by
// CompletionRequestedEvent (emitted right after the transition into Completing);
// any other event is ignored so a stray wake-up cannot finalize the agent.
func (s *CompletingState) Handle(event domain.AgentEvent) error {
	switch event.(type) {
	case domain.CompletionRequestedEvent:
		return s.complete()
	}
	return nil
}

// complete performs the final queue check and finalizes the agent execution: if
// messages were queued during completion it restarts the loop (CheckingQueue),
// otherwise it publishes the final completion event and transitions to Idle.
func (s *CompletingState) complete() error {
	logger.Debug("completing state: finalizing agent execution",
		"total_turns", s.ctx.AgentCtx.Turns,
		"queue_empty", s.ctx.AgentCtx.MessageQueue.IsEmpty())

	logger.Debug("sleeping 100ms for final queue check")
	time.Sleep(100 * time.Millisecond)

	if !s.ctx.AgentCtx.MessageQueue.IsEmpty() {
		logger.Debug("messages queued after completion, restarting agent")
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateCheckingQueue); err != nil {
			logger.Error("failed to transition to checking queue", "error", err)
			return err
		}
		s.ctx.Events <- domain.MessageReceivedEvent{}
		return nil
	}

	logger.Debug("no queued messages, completing agent execution")

	logger.Debug("publishing final chat completion event")
	if s.ctx.AgentCtx.Ctx.Err() != nil {
		s.ctx.PublishChatCancelled(s.ctx.GetMetrics(s.ctx.Request.RequestID))
	} else {
		s.ctx.PublishChatComplete("", []sdk.ChatCompletionMessageToolCall{}, s.ctx.GetMetrics(s.ctx.Request.RequestID))
	}

	if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateIdle); err != nil {
		logger.Error("failed to transition to idle", "error", err)
		return err
	}
	logger.Debug("agent execution completed successfully", "total_turns", s.ctx.AgentCtx.Turns)

	return nil
}
