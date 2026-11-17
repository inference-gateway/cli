package states

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// CheckingQueueState picks the next step: answer pending tool results, drain
// queued messages, complete, or keep looping. Chat turns never wait here for
// background work, since the UI starts fresh turns. Headless runs wait at the
// completion boundary so they never exit with orphaned background tasks.
type CheckingQueueState struct {
	ctx *StateContext
}

// NewCheckingQueueState creates a new CheckingQueue state handler
func NewCheckingQueueState(ctx *StateContext) StateHandler {
	return &CheckingQueueState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *CheckingQueueState) Name() AgentExecutionState {
	return StateCheckingQueue
}

// Handle processes events in CheckingQueue state
func (s *CheckingQueueState) Handle(event AgentEvent) error {
	switch event.(type) {
	case MessageReceivedEvent:
		logger.Debug("checking queue state: evaluating conditions",
			"turns", s.ctx.AgentCtx.Turns,
			"has_tool_results", s.ctx.AgentCtx.HasToolResults,
			"queue_empty", s.ctx.AgentCtx.MessageQueue.IsEmpty())

		if s.ctx.AgentCtx.HasToolResults {
			logger.Debug("has tool results - must respond to tools before processing queue")
			if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateStreamingLLM); err != nil {
				logger.Error("failed to transition to streaming llm", "error", err)
				return err
			}

			s.ctx.Events <- StartStreamingEvent{}
			return nil
		}

		s.drainQueueWithHooks()

		if !s.ctx.Request.IsChatMode && s.ctx.WaitForBackgroundTasks != nil &&
			s.ctx.AgentCtx.Ctx.Err() == nil &&
			s.ctx.StateMachine.CanTransition(s.ctx.AgentCtx, StateCompleting) {
			s.ctx.WaitForBackgroundTasks()
			s.drainQueueWithHooks()
		}

		if s.ctx.AgentCtx.Ctx.Err() != nil {
			logger.Debug("session cancelled - completing without next turn",
				"err", s.ctx.AgentCtx.Ctx.Err())
			if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCompleting); err != nil {
				logger.Error("failed to transition to completing", "error", err)
				return err
			}
			s.ctx.Events <- CompletionRequestedEvent{}
			return nil
		}

		if s.ctx.StateMachine.CanTransition(s.ctx.AgentCtx, StateCompleting) {
			logger.Debug("completion conditions met",
				"turns", s.ctx.AgentCtx.Turns,
				"has_tool_results", s.ctx.AgentCtx.HasToolResults,
				"queue_empty", s.ctx.AgentCtx.MessageQueue.IsEmpty(),
				"last_message_role", func() string {
					if len(*s.ctx.AgentCtx.Conversation) > 0 {
						return string((*s.ctx.AgentCtx.Conversation)[len(*s.ctx.AgentCtx.Conversation)-1].Role)
					}
					return "none"
				}())

			if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateCompleting); err != nil {
				logger.Error("failed to transition to completing", "error", err)
				return err
			}
			s.ctx.Events <- CompletionRequestedEvent{}
			return nil
		}

		logger.Debug("cannot complete yet, continuing agent loop",
			"turns", s.ctx.AgentCtx.Turns,
			"has_tool_results", s.ctx.AgentCtx.HasToolResults,
			"queue_empty", s.ctx.AgentCtx.MessageQueue.IsEmpty(),
			"last_message_role", func() string {
				if len(*s.ctx.AgentCtx.Conversation) > 0 {
					return string((*s.ctx.AgentCtx.Conversation)[len(*s.ctx.AgentCtx.Conversation)-1].Role)
				}
				return "none"
			}())

		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, StateStreamingLLM); err != nil {
			logger.Error("failed to transition to streaming llm", "error", err)
			return err
		}

		logger.Debug("emitting start streaming event", "turn", s.ctx.AgentCtx.Turns+1)
		s.ctx.Events <- StartStreamingEvent{}
	}
	return nil
}

// drainQueueWithHooks batches any queued messages into the conversation,
// dispatching the pre_queue_drain hook before and post_queue_drain after (the
// latter only when something was actually drained).
func (s *CheckingQueueState) drainQueueWithHooks() {
	if s.ctx.AgentCtx.MessageQueue.IsEmpty() {
		return
	}
	logger.Debug("queue not empty, draining")
	if s.ctx.DispatchHooks != nil {
		s.ctx.DispatchHooks(agentdomain.HookPreQueueDrain)
	}
	numBatched := s.ctx.BatchDrainQueue()
	logger.Debug("batched queued messages", "count", numBatched)
	if numBatched > 0 && s.ctx.DispatchHooks != nil {
		s.ctx.DispatchHooks(agentdomain.HookPostQueueDrain)
	}
}
