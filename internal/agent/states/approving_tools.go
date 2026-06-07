package states

import (
	"fmt"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// toolRound holds the per-round state for approving and executing a batch of
// tool calls. Approval prompts are handled one at a time, but each approved
// tool's execution is spawned immediately so executions overlap while the
// remaining tools are still being approved. Results are recorded into
// index-keyed slots and flushed to the conversation in tool-call order as each
// contiguous prefix completes, so the UI surfaces each tool result as the user
// approves the next one (the conversation validator requires tool-call order).
type toolRound struct {
	results []domain.ConversationEntry // indexed by tool position
	ready   []bool                     // ready[i] set once results[i] is filled
	flushed int                        // next slot index to flush, in order
	wg      sync.WaitGroup             // tracks spawned executions
	sem     chan struct{}              // bounds concurrent executions
}

// ApprovingToolsState handles events in the ApprovingTools state.
//
// This state manages sequential tool approval with overlapping execution:
//  1. MessageReceivedEvent → initializes the tool round, starts sequential approval
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

		round := &toolRound{
			results: make([]domain.ConversationEntry, len(*s.ctx.ToolsNeedingApproval)),
			ready:   make([]bool, len(*s.ctx.ToolsNeedingApproval)),
			sem:     make(chan struct{}, s.maxConcurrent()),
		}

		logger.Debug("starting tool approval", "total_tools", len(*s.ctx.ToolsNeedingApproval))

		s.ctx.WaitGroup.Add(1)
		go s.processNextTool(round)

	case domain.AllToolsProcessedEvent:
		logger.Debug("all tools processed", "results", len(*s.ctx.ToolResults))

		if err := s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StatePostToolExecution); err != nil {
			logger.Error("failed to transition to post tool execution", "error", err)
			return err
		}

		s.ctx.Events <- domain.MessageReceivedEvent{}

	case domain.ApprovalFailedEvent:
		logger.Error("approval failed", "error", e.Error)
		s.handleApprovalFailure(e.Error)
	}
	return nil
}

// processNextTool requests approval for ONE tool, then - if approved - spawns
// its execution in the background and immediately moves on to the next
// approval prompt so executions overlap. Fast-exits if the session context was
// cancelled, mirroring the drain-then-stop contract in CheckingQueue /
// PostToolExecution: the remaining approval prompts are skipped and control
// returns to the state machine via finishApprovals.
func (s *ApprovingToolsState) processNextTool(round *toolRound) {
	defer s.ctx.WaitGroup.Done()

	if s.ctx.AgentCtx.Ctx.Err() != nil {
		logger.Debug("session cancelled during approval loop", "err", s.ctx.AgentCtx.Ctx.Err())
		s.finishApprovals(round)
		return
	}

	tc, idx := s.getNextToolForProcessing()
	if tc == nil {
		s.finishApprovals(round)
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
		s.completeSlot(round, idx, s.buildRejectionEntry(*tc))
		s.continueToNextTool(round)
		return
	}

	logger.Debug("tool approved", "tool", tc.Function.Name)

	if s.shouldAutoApproveRemaining() {
		s.spawnAllRemaining(round, idx, *tc)
		s.finishApprovals(round)
		return
	}

	s.spawnExecution(round, idx, *tc)
	s.continueToNextTool(round)
}

// continueToNextTool advances the sequential approval loop to the next tool.
func (s *ApprovingToolsState) continueToNextTool(round *toolRound) {
	s.ctx.WaitGroup.Add(1)
	go s.processNextTool(round)
}

// getNextToolForProcessing returns the next tool that needs processing and its
// position (used as the result slot index).
func (s *ApprovingToolsState) getNextToolForProcessing() (*sdk.ChatCompletionMessageToolCall, int) {
	s.ctx.Mutex.Lock()
	defer s.ctx.Mutex.Unlock()

	if *s.ctx.CurrentToolIndex >= len(*s.ctx.ToolsNeedingApproval) {
		return nil, -1
	}

	idx := *s.ctx.CurrentToolIndex
	tc := &(*s.ctx.ToolsNeedingApproval)[idx]
	*s.ctx.CurrentToolIndex++
	return tc, idx
}

// spawnExecution runs an approved tool in the background, recording its result
// in the round's slot. Concurrency is bounded by the round semaphore.
func (s *ApprovingToolsState) spawnExecution(round *toolRound, idx int, tc sdk.ChatCompletionMessageToolCall) {
	round.wg.Add(1)
	go func() {
		defer round.wg.Done()
		round.sem <- struct{}{}
		defer func() { <-round.sem }()

		logger.Debug("executing approved tool", "tool", tc.Function.Name)
		s.completeSlot(round, idx, s.ctx.ExecuteToolInternal(tc, true))
	}()
}

// spawnAllRemaining executes the just-approved tool and every remaining tool in
// auto-accept mode. Each runs in the background (bounded by the semaphore);
// results are collected in tool-call order.
func (s *ApprovingToolsState) spawnAllRemaining(round *toolRound, idx int, tc sdk.ChatCompletionMessageToolCall) {
	logger.Debug("auto-accept mode enabled, auto-approving all remaining tools")

	s.spawnExecution(round, idx, tc)

	s.ctx.Mutex.Lock()
	start := *s.ctx.CurrentToolIndex
	tools := *s.ctx.ToolsNeedingApproval
	s.ctx.Mutex.Unlock()

	for i := start; i < len(tools); i++ {
		remaining := tools[i]
		logger.Debug("auto-approving tool", "tool", remaining.Function.Name)

		s.spawnExecution(round, i, remaining)
	}

	s.ctx.Mutex.Lock()
	*s.ctx.CurrentToolIndex = len(tools)
	s.ctx.Mutex.Unlock()
}

// completeSlot records a finished tool result (an executed tool or a rejection)
// in its slot and flushes any now-contiguous prefix of results. Flushing as
// results complete - rather than batching at the end - lets the UI surface each
// tool result as the user approves the next one, while still preserving
// tool-call order for the conversation.
func (s *ApprovingToolsState) completeSlot(round *toolRound, idx int, entry domain.ConversationEntry) {
	s.ctx.Mutex.Lock()
	defer s.ctx.Mutex.Unlock()

	round.results[idx] = entry
	round.ready[idx] = true
	s.flushLocked(round)
}

// flushReady flushes the contiguous-ready prefix of results. Used after
// WaitGroup.Wait to drain anything not yet flushed (e.g. on cancellation).
func (s *ApprovingToolsState) flushReady(round *toolRound) {
	s.ctx.Mutex.Lock()
	defer s.ctx.Mutex.Unlock()
	s.flushLocked(round)
}

// flushLocked appends every ready result starting at the flush cursor to the
// conversation, storage, and ToolResults, in tool-call order. The caller must
// hold s.ctx.Mutex.
func (s *ApprovingToolsState) flushLocked(round *toolRound) {
	for round.flushed < len(round.ready) && round.ready[round.flushed] {
		entry := round.results[round.flushed]
		round.flushed++

		*s.ctx.ToolResults = append(*s.ctx.ToolResults, entry)
		*s.ctx.AgentCtx.Conversation = append(*s.ctx.AgentCtx.Conversation, entry.Message)
		if err := s.ctx.AddMessage(entry); err != nil {
			logger.Error("failed to store tool result", "error", err)
		}
		s.ctx.AgentCtx.HasToolResults = true
	}
}

// finishApprovals waits for every spawned execution to complete, drains any
// remaining results, and signals the state machine. Results are flushed
// incrementally by completeSlot as they complete; this final drain covers
// gaps left by cancellation.
func (s *ApprovingToolsState) finishApprovals(round *toolRound) {
	round.wg.Wait()
	s.flushReady(round)

	s.ctx.Events <- domain.AllToolsProcessedEvent{}
}

// buildRejectionEntry constructs the Tool-role result for a user-rejected tool
// and publishes the rejection event. The entry is appended to the conversation
// in order by the flush.
func (s *ApprovingToolsState) buildRejectionEntry(tc sdk.ChatCompletionMessageToolCall) domain.ConversationEntry {
	logger.Debug("tool rejected by user", "tool", tc.Function.Name)

	rejectionMessage := sdk.Message{
		Role:       sdk.Tool,
		Content:    sdk.NewMessageContent(fmt.Sprintf("Tool execution rejected by user: %s", tc.Function.Name)),
		ToolCallID: &tc.ID,
	}

	return domain.ConversationEntry{
		Message: rejectionMessage,
		Time:    time.Now(),
	}
}

// shouldAutoApproveRemaining checks if auto-accept mode is enabled
func (s *ApprovingToolsState) shouldAutoApproveRemaining() bool {
	return s.ctx.GetAgentMode() == domain.AgentModeAutoAccept
}

// maxConcurrent returns the bound on concurrently executing tools, clamped to
// at least 1 so a missing/zero config value cannot deadlock the semaphore.
func (s *ApprovingToolsState) maxConcurrent() int {
	if s.ctx.MaxConcurrentTools < 1 {
		return 1
	}
	return s.ctx.MaxConcurrentTools
}

// handleApprovalFailure handles when approval fails (timeout, error, etc.)
func (s *ApprovingToolsState) handleApprovalFailure(err error) {
	logger.Error("handling approval failure", "error", err)

	s.ctx.PublishChatEvent(domain.ChatErrorEvent{
		RequestID: s.ctx.Request.RequestID,
		Timestamp: time.Now(),
		Error:     fmt.Errorf("approval failed: %w", err),
	})

	_ = s.ctx.StateMachine.Transition(s.ctx.AgentCtx, domain.StateError)
}
