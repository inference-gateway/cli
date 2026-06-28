package states

import (
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
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
			logger.Debug("tool call", "tool", tc.Function.Name, "id", tc.ID)
		}
	}
	s.ctx.PublishChatComplete(*s.ctx.CurrentReasoning, completeToolCalls, s.ctx.GetMetrics(s.ctx.Request.RequestID))

	needsApproval := false
	allApprovalBlocked := true
	for _, toolCall := range *s.ctx.CurrentToolCalls {
		if !s.ctx.ShouldRequireApproval(toolCall, s.ctx.Request.IsChatMode) {
			continue
		}
		needsApproval = true
		if s.approvalBlocked(toolCall) {
			logger.Debug("tool requires approval but delivery is blocked", "tool", toolCall.Function.Name)
		} else {
			allApprovalBlocked = false
			logger.Debug("tool requires approval", "tool", toolCall.Function.Name)
		}
	}

	if (!needsApproval || !allApprovalBlocked) && s.ctx.DispatchHooks != nil {
		s.ctx.DispatchHooks(domain.HookPreTool)
	}

	switch {
	case needsApproval && allApprovalBlocked:
		logger.Info("approval required but undeliverable, blocking tools", "tool_count", len(*s.ctx.CurrentToolCalls))
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateBlockingTools); err != nil {
			logger.Error("failed to transition to blocking tools", "error", err)
			return err
		}
		s.ctx.Events <- domain.MessageReceivedEvent{}
	case needsApproval:
		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateApprovingTools); err != nil {
			logger.Error("failed to transition to approving tools", "error", err)
			return err
		}
		s.ctx.Events <- domain.MessageReceivedEvent{}
	default:
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

// approvalBlocked reports whether a tool that needs approval cannot have that
// approval delivered in this session (approval_behaviour resolves to block, e.g.
// approval_behaviour=block, or =ipc with no broker). Such tools are routed to
// BlockingTools instead of being prompted. Falls back to false (promptable) when
// no delivery resolver is wired.
func (s *EvaluatingToolsState) approvalBlocked(toolCall *sdk.ChatCompletionMessageToolCall) bool {
	if s.ctx.ApprovalDelivery == nil {
		return false
	}
	return s.ctx.ApprovalDelivery(toolCall) == config.ApprovalBehaviourBlock
}
