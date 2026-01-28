package states

import (
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// PostStreamState handles events in the PostStream state.
//
// This state:
//  1. Stores assistant message to conversation
//  2. Checks if messages were queued during stream → CheckingQueue
//  3. If tool calls exist → EvaluatingTools
//  4. If no tools and can complete → Completing
//  5. Otherwise → CheckingQueue
type PostStreamState struct {
	ctx *domain.StateContext
}

// NewPostStreamState creates a new PostStream state handler
func NewPostStreamState(ctx *domain.StateContext) domain.StateHandler {
	return &PostStreamState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *PostStreamState) Name() domain.AgentExecutionState {
	return domain.StatePostStream
}

// Handle processes events in PostStream state
func (s *PostStreamState) Handle(event domain.AgentEvent) error {
	logger.Debug("post stream state: evaluating next action",
		"turn", s.ctx.AgentCtx.Turns,
		"tool_calls", len(*s.ctx.CurrentToolCalls),
		"queue_empty", s.ctx.AgentCtx.MessageQueue.IsEmpty())

	logger.Debug("post stream: assistant message already in conversation from streaming",
		"has_tool_calls", len(*s.ctx.CurrentToolCalls) > 0,
		"has_reasoning", *s.ctx.CurrentReasoning != "")

	if !s.ctx.AgentCtx.MessageQueue.IsEmpty() {
		logger.Debug("messages queued during stream, returning to checking queue")
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateCheckingQueue); err != nil {
			logger.Error("failed to transition to checking queue", "error", err)
			return err
		}
		s.ctx.Events <- domain.MessageReceivedEvent{}
		return nil
	}

	if len(*s.ctx.CurrentToolCalls) > 0 {
		return s.transitionToEvaluatingTools()
	}

	return s.handleNoToolCallsScenario()
}

// transitionToEvaluatingTools transitions to tool evaluation state
func (s *PostStreamState) transitionToEvaluatingTools() error {
	logger.Debug("has tool calls, evaluating tools", "count", len(*s.ctx.CurrentToolCalls))
	if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateEvaluatingTools); err != nil {
		logger.Error("failed to transition to evaluating tools", "error", err)
		return err
	}
	s.ctx.Events <- domain.MessageReceivedEvent{}
	return nil
}

// handleNoToolCallsScenario handles the scenario when there are no tool calls
func (s *PostStreamState) handleNoToolCallsScenario() error {
	s.ctx.AgentCtx.HasToolResults = false
	logger.Debug("No tool calls in response")

	canComplete := s.ctx.AgentCtx.Turns > 0 && s.ctx.AgentCtx.MessageQueue.IsEmpty()
	if canComplete {
		return s.transitionToCompleting()
	}

	return s.transitionToCheckingQueue()
}

// transitionToCompleting transitions to completing state
func (s *PostStreamState) transitionToCompleting() error {
	logger.Debug("agent can complete (no tools, turns > 0, queue empty)")

	var completeToolCalls []sdk.ChatCompletionMessageToolCall
	s.ctx.PublishChatComplete(*s.ctx.CurrentReasoning, completeToolCalls, s.ctx.GetMetrics(s.ctx.Request.RequestID))

	if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateCompleting); err != nil {
		logger.Error("failed to transition to completing", "error", err)
		return err
	}
	s.ctx.Events <- domain.CompletionRequestedEvent{}
	return nil
}

// transitionToCheckingQueue transitions back to checking queue state
func (s *PostStreamState) transitionToCheckingQueue() error {
	logger.Debug("continuing agent loop (need more turns)")
	if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateCheckingQueue); err != nil {
		logger.Error("failed to transition to checking queue", "error", err)
		return err
	}
	s.ctx.Events <- domain.MessageReceivedEvent{}
	return nil
}
