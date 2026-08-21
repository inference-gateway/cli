package states

import (
	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
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
	ctx *StateContext
}

// NewPostStreamState creates a new PostStream state handler
func NewPostStreamState(ctx *StateContext) StateHandler {
	return &PostStreamState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *PostStreamState) Name() AgentExecutionState {
	return StatePostStream
}

// Handle processes events in PostStream state
func (s *PostStreamState) Handle(event AgentEvent) error {
	logger.Debug("post stream state: evaluating next action",
		"turn", s.ctx.AgentCtx.Turns,
		"tool_calls", len(*s.ctx.CurrentToolCalls),
		"queue_empty", s.ctx.AgentCtx.MessageQueue.IsEmpty())

	logger.Debug("post stream: assistant message already in conversation from streaming",
		"has_tool_calls", len(*s.ctx.CurrentToolCalls) > 0,
		"has_reasoning", *s.ctx.CurrentReasoning != "")

	if s.ctx.DispatchHooks != nil {
		s.ctx.DispatchHooks(agentdomain.HookPostStream)
	}

	if !s.ctx.AgentCtx.MessageQueue.IsEmpty() {
		logger.Debug("messages queued during stream, returning to checking queue")
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCheckingQueue); err != nil {
			logger.Error("failed to transition to checking queue", "error", err)
			return err
		}
		s.ctx.Events <- MessageReceivedEvent{}
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
	if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateEvaluatingTools); err != nil {
		logger.Error("failed to transition to evaluating tools", "error", err)
		return err
	}
	s.ctx.Events <- MessageReceivedEvent{}
	return nil
}

// handleNoToolCallsScenario handles the scenario when there are no tool calls
func (s *PostStreamState) handleNoToolCallsScenario() error {
	s.ctx.AgentCtx.HasToolResults = false
	logger.Debug("no tool calls in response")

	if s.ctx.StateMachine.CanTransition(s.ctx.AgentCtx, StateCompleting) {
		return s.transitionToCompleting()
	}

	return s.transitionToStreaming()
}

// transitionToCompleting transitions to completing state
func (s *PostStreamState) transitionToCompleting() error {
	logger.Debug("agent can complete (no tools, turns > 0, queue empty)")

	var completeToolCalls []sdk.ChatCompletionMessageToolCall
	s.ctx.PublishChatComplete(*s.ctx.CurrentReasoning, completeToolCalls, s.ctx.GetMetrics(s.ctx.Request.RequestID))

	if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCompleting); err != nil {
		logger.Error("failed to transition to completing", "error", err)
		return err
	}
	s.ctx.Events <- CompletionRequestedEvent{}
	return nil
}

// transitionToStreaming starts another LLM turn. Reached when completion is
// not possible yet with an empty queue - e.g. a hook appended a hidden
// user-role reminder to the conversation, which the model must answer.
func (s *PostStreamState) transitionToStreaming() error {
	logger.Debug("continuing agent loop (need more turns)")
	if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateStreamingLLM); err != nil {
		logger.Error("failed to transition to streaming", "error", err)
		return err
	}
	s.ctx.Events <- StartStreamingEvent{}
	return nil
}
