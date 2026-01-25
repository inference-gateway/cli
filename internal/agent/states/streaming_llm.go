package states

import (
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// StreamingLLMState handles events in the StreamingLLM state.
//
// This state manages LLM streaming:
//  1. StartStreamingEvent → launches background streaming goroutine
//  2. StreamCompletedEvent → processes completed stream, stores data, transitions to PostStream
type StreamingLLMState struct {
	ctx *domain.StateContext
}

// NewStreamingLLMState creates a new StreamingLLM state handler
func NewStreamingLLMState(ctx *domain.StateContext) domain.StateHandler {
	return &StreamingLLMState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *StreamingLLMState) Name() domain.AgentExecutionState {
	return domain.StateStreamingLLM
}

// Handle processes events in StreamingLLM state
func (s *StreamingLLMState) Handle(event domain.AgentEvent) error {
	switch e := event.(type) {
	case domain.StartStreamingEvent:
		logger.Debug("starting llm streaming (background goroutine)", "turn", s.ctx.AgentCtx.Turns+1)
		s.ctx.WaitGroup.Add(1)
		go func() {
			defer s.ctx.WaitGroup.Done()
			s.ctx.StartStreaming()
		}()

	case domain.StreamCompletedEvent:
		logger.Debug("stream completed",
			"turn", s.ctx.AgentCtx.Turns,
			"tool_calls", len(e.ToolCalls),
			"has_reasoning", e.Reasoning != "")

		*s.ctx.CurrentMessage = e.Message
		*s.ctx.CurrentToolCalls = e.ToolCalls
		*s.ctx.CurrentReasoning = e.Reasoning
		s.ctx.AgentCtx.ToolCalls = e.ToolCalls

		contentStr, _ := e.Message.Content.AsMessageContent0()
		logger.Debug("Storing stream data",
			"content_length", len(contentStr),
			"reasoning_length", len(e.Reasoning))

		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StatePostStream); err != nil {
			logger.Error("failed to transition to post stream", "error", err)
			return err
		}

		s.ctx.Events <- domain.MessageReceivedEvent{}
	}
	return nil
}
