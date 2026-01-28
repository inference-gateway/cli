package states

import (
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// ApprovingToolsState handles events in the ApprovingTools state.
//
// This state manages sequential tool approval:
//  1. MessageReceivedEvent → initializes tool processing queue, starts sequential approval
//  2. AllToolsProcessedEvent → transitions to PostToolExecution
//  3. ApprovalFailedEvent → handles approval failures
type ApprovingToolsState struct {
	ctx *domain.StateContext
}

// NewApprovingToolsState creates a new ApprovingTools state handler
func NewApprovingToolsState(ctx *domain.StateContext) domain.StateHandler {
	return &ApprovingToolsState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *ApprovingToolsState) Name() domain.AgentExecutionState {
	return domain.StateApprovingTools
}

// Handle processes events in ApprovingTools state
func (s *ApprovingToolsState) Handle(event domain.AgentEvent) error {
	switch e := event.(type) {
	case domain.MessageReceivedEvent:
		logger.Debug("approving tools state: initializing tool processing queue")

		*s.ctx.ToolsNeedingApproval = make([]sdk.ChatCompletionMessageToolCall, 0, len(*s.ctx.CurrentToolCalls))
		for _, tc := range *s.ctx.CurrentToolCalls {
			*s.ctx.ToolsNeedingApproval = append(*s.ctx.ToolsNeedingApproval, *tc)
		}
		*s.ctx.CurrentToolIndex = 0
		*s.ctx.ToolResults = []domain.ConversationEntry{}

		logger.Debug("starting sequential tool approval", "total_tools", len(*s.ctx.ToolsNeedingApproval))

		s.ctx.WaitGroup.Add(1)
		go s.processNextTool()

	case domain.AllToolsProcessedEvent:
		logger.Debug("all tools processed", "results", len(*s.ctx.ToolResults))

		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StatePostToolExecution); err != nil {
			logger.Error("failed to transition to post tool execution", "error", err)
			return err
		}

		s.ctx.Events <- domain.MessageReceivedEvent{}

	case domain.ApprovalFailedEvent:
		logger.Error("Approval failed", "error", e.Error)
		s.handleApprovalFailure(e.Error)
	}
	return nil
}

// processNextTool handles approval and execution of ONE tool sequentially
func (s *ApprovingToolsState) processNextTool() {
	defer s.ctx.WaitGroup.Done()

	tc := s.getNextToolForProcessing()
	if tc == nil {
		logger.Debug("all tools processed", "approved", len(*s.ctx.ToolResults))
		s.ctx.Events <- domain.AllToolsProcessedEvent{}
		return
	}

	logger.Debug("requesting approval for tool", "tool", tc.Function.Name)

	approved, err := s.ctx.RequestToolApproval(*tc)
	if err != nil {
		logger.Error("approval request failed", "tool", tc.Function.Name, "error", err)
		s.ctx.Events <- domain.ApprovalFailedEvent{Error: err}
		return
	}

	if !approved {
		s.handleToolRejection(*tc)
		s.ctx.WaitGroup.Add(1)
		go s.processNextTool()
		return
	}

	logger.Debug("tool approved", "tool", tc.Function.Name)
	s.ctx.PublishChatEvent(domain.ToolApprovedEvent{
		RequestID: s.ctx.Request.RequestID,
		Timestamp: time.Now(),
		ToolCall:  *tc,
	})

	if s.shouldAutoApproveRemaining() {
		s.executeAllRemainingTools(*tc)
		s.ctx.Events <- domain.AllToolsProcessedEvent{}
		return
	}

	s.executeSingleApprovedTool(*tc)

	s.ctx.WaitGroup.Add(1)
	go s.processNextTool()
}

// getNextToolForProcessing returns the next tool that needs processing
func (s *ApprovingToolsState) getNextToolForProcessing() *sdk.ChatCompletionMessageToolCall {
	s.ctx.Mutex.Lock()
	defer s.ctx.Mutex.Unlock()

	if *s.ctx.CurrentToolIndex >= len(*s.ctx.ToolsNeedingApproval) {
		return nil
	}

	tc := &(*s.ctx.ToolsNeedingApproval)[*s.ctx.CurrentToolIndex]
	*s.ctx.CurrentToolIndex++
	return tc
}

// handleToolRejection handles when a user rejects a tool call
func (s *ApprovingToolsState) handleToolRejection(tc sdk.ChatCompletionMessageToolCall) {
	logger.Debug("tool rejected by user", "tool", tc.Function.Name)

	rejectionMessage := sdk.Message{
		Role:       sdk.Tool,
		Content:    sdk.NewMessageContent(fmt.Sprintf("Tool execution rejected by user: %s", tc.Function.Name)),
		ToolCallId: &tc.Id,
	}

	*s.ctx.AgentCtx.Conversation = append(*s.ctx.AgentCtx.Conversation, rejectionMessage)

	rejectionEntry := domain.ConversationEntry{
		Message: rejectionMessage,
		Time:    time.Now(),
	}

	if err := s.ctx.AddMessage(rejectionEntry); err != nil {
		logger.Error("failed to store tool rejection message", "error", err)
	}

	s.ctx.PublishChatEvent(domain.ToolRejectedEvent{
		RequestID: s.ctx.Request.RequestID,
		Timestamp: time.Now(),
		ToolCall:  tc,
	})

	s.ctx.Mutex.Lock()
	s.ctx.AgentCtx.HasToolResults = true
	s.ctx.Mutex.Unlock()
}

// shouldAutoApproveRemaining checks if auto-accept mode is enabled
func (s *ApprovingToolsState) shouldAutoApproveRemaining() bool {
	return s.ctx.GetAgentMode() == domain.AgentModeAutoAccept
}

// executeAllRemainingTools executes the current tool and all remaining tools in auto-accept mode
func (s *ApprovingToolsState) executeAllRemainingTools(tc sdk.ChatCompletionMessageToolCall) {
	logger.Debug("auto-accept mode enabled, auto-approving all remaining tools")

	s.ctx.Mutex.Lock()
	remainingTools := (*s.ctx.ToolsNeedingApproval)[*s.ctx.CurrentToolIndex:]
	s.ctx.Mutex.Unlock()

	for _, remainingTool := range remainingTools {
		logger.Debug("auto-approving tool", "tool", remainingTool.Function.Name)

		s.ctx.PublishChatEvent(domain.ToolApprovedEvent{
			RequestID: s.ctx.Request.RequestID,
			Timestamp: time.Now(),
			ToolCall:  remainingTool,
		})

		result := s.ctx.ExecuteToolInternal(remainingTool, true)
		s.appendToolResult(result)
	}

	result := s.ctx.ExecuteToolInternal(tc, true)
	s.appendToolResult(result)

	s.ctx.Mutex.Lock()
	*s.ctx.CurrentToolIndex = len(*s.ctx.ToolsNeedingApproval)
	s.ctx.Mutex.Unlock()
}

// executeSingleApprovedTool executes a single approved tool
func (s *ApprovingToolsState) executeSingleApprovedTool(tc sdk.ChatCompletionMessageToolCall) {
	logger.Debug("executing approved tool", "tool", tc.Function.Name)

	result := s.ctx.ExecuteToolInternal(tc, true)
	s.appendToolResult(result)
}

// appendToolResult appends a tool execution result to the conversation and storage
func (s *ApprovingToolsState) appendToolResult(result domain.ConversationEntry) {
	s.ctx.Mutex.Lock()
	*s.ctx.ToolResults = append(*s.ctx.ToolResults, result)
	s.ctx.Mutex.Unlock()

	*s.ctx.AgentCtx.Conversation = append(*s.ctx.AgentCtx.Conversation, result.Message)

	if err := s.ctx.AddMessage(result); err != nil {
		logger.Error("failed to store tool result", "error", err)
	}

	s.ctx.Mutex.Lock()
	s.ctx.AgentCtx.HasToolResults = true
	s.ctx.Mutex.Unlock()
}

// handleApprovalFailure handles when approval fails (timeout, error, etc.)
func (s *ApprovingToolsState) handleApprovalFailure(err error) {
	logger.Error("Handling approval failure", "error", err)

	s.ctx.PublishChatEvent(domain.ChatErrorEvent{
		RequestID: s.ctx.Request.RequestID,
		Timestamp: time.Now(),
		Error:     fmt.Errorf("approval failed: %w", err),
	})

	_ = s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateError)
}
