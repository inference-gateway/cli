package states

import (
	"time"

	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// CheckingQueueState handles events in the CheckingQueue state.
//
// This state evaluates multiple conditions to determine the next action:
//  1. Tool results pending → must respond to tools first (StreamingLLM)
//  2. Messages queued → drain queue into conversation
//  3. Background tasks pending → wait for completion
//  4. Can complete → transition to Completing
//  5. Otherwise → continue agent loop (StreamingLLM)
type CheckingQueueState struct {
	ctx *domain.StateContext
}

// NewCheckingQueueState creates a new CheckingQueue state handler
func NewCheckingQueueState(ctx *domain.StateContext) domain.StateHandler {
	return &CheckingQueueState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *CheckingQueueState) Name() domain.AgentExecutionState {
	return domain.StateCheckingQueue
}

// Handle processes events in CheckingQueue state
func (s *CheckingQueueState) Handle(event domain.AgentEvent) error {
	switch event.(type) {
	case domain.MessageReceivedEvent:
		logger.Debug("checking queue state: evaluating conditions",
			"turns", s.ctx.AgentCtx.Turns,
			"has_tool_results", s.ctx.AgentCtx.HasToolResults,
			"queue_empty", s.ctx.AgentCtx.MessageQueue.IsEmpty())

		if s.ctx.AgentCtx.HasToolResults {
			logger.Debug("has tool results - must respond to tools before processing queue")
			if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateStreamingLLM); err != nil {
				logger.Error("failed to transition to streaming llm", "error", err)
				return err
			}

			s.ctx.Events <- domain.StartStreamingEvent{}
			return nil
		}

		if !s.ctx.AgentCtx.MessageQueue.IsEmpty() {
			logger.Debug("queue not empty, draining")
			numBatched := s.ctx.BatchDrainQueue()
			logger.Debug("batched queued messages", "count", numBatched)
		}

		if s.ctx.TaskTracker != nil && len(s.ctx.TaskTracker.GetAllPollingTasks()) > 0 {
			numTasks := len(s.ctx.TaskTracker.GetAllPollingTasks())
			logger.Debug("background tasks pending, waiting", "tasks", numTasks)
			s.ctx.WaitGroup.Add(1)
			go func() {
				defer s.ctx.WaitGroup.Done()
				select {
				case <-time.After(constants.BackgroundTaskPollDelay):
					select {
					case s.ctx.Events <- domain.MessageReceivedEvent{}:
					case <-s.ctx.CancelChan:
						return
					}
				case <-s.ctx.CancelChan:
					return
				}
			}()
			return nil
		}

		if s.ctx.StateMachine.CanTransition(s.ctx.AgentCtx, domain.StateCompleting) {
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

			if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateCompleting); err != nil {
				logger.Error("failed to transition to completing", "error", err)
				return err
			}
			s.ctx.Events <- domain.CompletionRequestedEvent{}
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

		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateStreamingLLM); err != nil {
			logger.Error("failed to transition to streaming llm", "error", err)
			return err
		}

		logger.Debug("emitting start streaming event", "turn", s.ctx.AgentCtx.Turns+1)
		s.ctx.Events <- domain.StartStreamingEvent{}
	}
	return nil
}
