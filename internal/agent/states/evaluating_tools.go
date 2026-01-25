package states

import (
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// EvaluatingToolsState handles events in the EvaluatingTools state.
//
// This state:
//  1. Publishes chat complete event with tool calls
//  2. Checks if any tool requires approval
//  3. If approval needed → ApprovingTools
//  4. Otherwise → ExecutingTools (starts background execution)
type EvaluatingToolsState struct {
	ctx *domain.StateContext
}

// NewEvaluatingToolsState creates a new EvaluatingTools state handler
func NewEvaluatingToolsState(ctx *domain.StateContext) domain.StateHandler {
	return &EvaluatingToolsState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *EvaluatingToolsState) Name() domain.AgentExecutionState {
	return domain.StateEvaluatingTools
}

// Handle processes events in EvaluatingTools state
func (s *EvaluatingToolsState) Handle(event domain.AgentEvent) error {
	logger.Debug("evaluating tools", "tool_count", len(*s.ctx.CurrentToolCalls))

	var completeToolCalls []sdk.ChatCompletionMessageToolCall
	if len(*s.ctx.CurrentToolCalls) > 0 {
		completeToolCalls = make([]sdk.ChatCompletionMessageToolCall, 0, len(*s.ctx.CurrentToolCalls))
		for _, tc := range *s.ctx.CurrentToolCalls {
			completeToolCalls = append(completeToolCalls, *tc)
			logger.Debug("Tool call", "tool", tc.Function.Name, "id", tc.Id)
		}
	}
	s.ctx.PublishChatComplete(*s.ctx.CurrentReasoning, completeToolCalls, s.ctx.GetMetrics(s.ctx.Request.RequestID))

	needsApproval := false
	for _, toolCall := range *s.ctx.CurrentToolCalls {
		if s.ctx.ShouldRequireApproval(toolCall, s.ctx.Request.IsChatMode) {
			needsApproval = true
			logger.Debug("Tool requires approval", "tool", toolCall.Function.Name)
			break
		}
	}

	if needsApproval {
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateApprovingTools); err != nil {
			logger.Error("failed to transition to approving tools", "error", err)
			return err
		}
		s.ctx.Events <- domain.MessageReceivedEvent{}
	} else {
		logger.Debug("no approval needed, executing tools")
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateExecutingTools); err != nil {
			logger.Error("failed to transition to executing tools", "error", err)
			return err
		}

		s.ctx.WaitGroup.Add(1)
		go (*s.ctx.ToolExecutor)()
	}

	return nil
}
