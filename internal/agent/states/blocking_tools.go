package states

import (
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// BlockingToolsState handles events in the BlockingTools state.
//
// This state is entered (from EvaluatingTools) when at least one tool call
// requires approval but that approval cannot be delivered in the current session
// (approval_behaviour resolves to block - e.g. approval_behaviour=block, or =ipc
// with no broker attached). There is no approver to prompt, so each gated tool is
// rejected with an actionable reason instead of being executed. Tool calls in the
// same batch that do NOT require approval (e.g. read-only tools, allow-listed
// Bash) still run, preserving tool-call order.
//
//  1. MessageReceivedEvent → processes the batch, then emits AllToolsProcessedEvent
//  2. AllToolsProcessedEvent → transitions to PostToolExecution
type BlockingToolsState struct {
	ctx *domain.StateContext
}

// NewBlockingToolsState creates a new BlockingTools state handler
func NewBlockingToolsState(ctx *domain.StateContext) domain.StateHandler {
	return &BlockingToolsState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *BlockingToolsState) Name() domain.AgentExecutionState {
	return domain.StateBlockingTools
}

// Handle processes events in BlockingTools state
func (s *BlockingToolsState) Handle(event domain.AgentEvent) error {
	switch event.(type) {
	case domain.MessageReceivedEvent:
		logger.Info("blocking tools state: rejecting gated tools (no approver reachable)",
			"tool_count", len(*s.ctx.CurrentToolCalls))

		*s.ctx.CurrentToolIndex = 0
		*s.ctx.ToolResults = []domain.ConversationEntry{}

		s.ctx.WaitGroup.Add(1)
		go s.processBlockedTools()

	case domain.AllToolsProcessedEvent:
		logger.Debug("blocking tools processed", "results", len(*s.ctx.ToolResults))

		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StatePostToolExecution); err != nil {
			logger.Error("failed to transition to post tool execution", "error", err)
			return err
		}

		s.ctx.Events <- domain.MessageReceivedEvent{}
	}
	return nil
}

// processBlockedTools walks the batch in tool-call order. Tools that need
// approval are turned into a "blocked" tool result (the LLM sees the reason);
// tools that do not need approval are executed normally so a mixed batch is not
// penalised. Results are appended in order, mirroring the conversation-validator
// contract used by ApprovingTools.
func (s *BlockingToolsState) processBlockedTools() {
	defer s.ctx.WaitGroup.Done()

	for _, tc := range *s.ctx.CurrentToolCalls {
		if s.ctx.AgentCtx.Ctx.Err() != nil {
			logger.Debug("session cancelled during blocking loop", "err", s.ctx.AgentCtx.Ctx.Err())
			break
		}

		entry := s.resolveEntry(tc)

		s.ctx.Mutex.Lock()
		*s.ctx.ToolResults = append(*s.ctx.ToolResults, entry)
		*s.ctx.AgentCtx.Conversation = append(*s.ctx.AgentCtx.Conversation, entry.Message)
		if err := s.ctx.AddMessage(entry); err != nil {
			logger.Error("failed to store blocked tool result", "error", err)
		}
		s.ctx.AgentCtx.HasToolResults = true
		s.ctx.Mutex.Unlock()
	}

	s.ctx.AgentCtx.LastToolFailed = domain.AnyToolFailed(*s.ctx.ToolResults)
	s.ctx.Events <- domain.AllToolsProcessedEvent{}
}

// resolveEntry executes a non-gated tool or builds a blocked result for a gated
// one.
func (s *BlockingToolsState) resolveEntry(tc *sdk.ChatCompletionMessageToolCall) domain.ConversationEntry {
	if !s.ctx.ShouldRequireApproval(tc, s.ctx.Request.IsChatMode) {
		logger.Debug("blocking state: running non-gated tool", "tool", tc.Function.Name)
		return s.ctx.ExecuteToolInternal(*tc, false)
	}
	return s.buildBlockedEntry(*tc)
}

// buildBlockedEntry constructs the Tool-role result for a tool that requires
// approval no approver can deliver. It also publishes a terminal tool-execution
// progress event so the live streaming overlay (which left the call at "ready" ->
// rendered as "queued") is cleared instead of lingering forever.
func (s *BlockingToolsState) buildBlockedEntry(tc sdk.ChatCompletionMessageToolCall) domain.ConversationEntry {
	reason := fmt.Sprintf(
		"%s requires approval, but approvals are not available in this session (tools.safety.approval_behaviour). "+
			"The action was NOT executed. Do not retry the same call - tell the user what you need and why, "+
			"or use a tool or command that does not require approval.",
		tc.Function.Name,
	)
	content := "Blocked: " + reason

	logger.Info("tool blocked (approval required, no approver reachable)", "tool", tc.Function.Name)

	s.ctx.PublishChatEvent(domain.ToolExecutionProgressEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: s.ctx.Request.RequestID,
			Timestamp: time.Now(),
		},
		ToolCallID: tc.ID,
		ToolName:   tc.Function.Name,
		Status:     "failed",
		Message:    "blocked",
	})

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		args = make(map[string]any)
	}

	return domain.ConversationEntry{
		Message: sdk.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent(content),
			ToolCallID: &tc.ID,
		},
		Time: time.Now(),
		ToolExecution: &domain.ToolExecutionResult{
			ToolName:  tc.Function.Name,
			Arguments: args,
			Success:   false,
			Error:     reason,
			Rejected:  true,
		},
	}
}
