package states_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	states "github.com/inference-gateway/cli/internal/agent/states"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func makeTools(n int) []*sdk.ChatCompletionMessageToolCall {
	tools := make([]*sdk.ChatCompletionMessageToolCall, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("call-%d", i)
		tools[i] = &sdk.ChatCompletionMessageToolCall{
			ID:       id,
			Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: "{}"},
		}
	}
	return tools
}

func toolEntry(tc sdk.ChatCompletionMessageToolCall) convdomain.ConversationEntry {
	id := tc.ID
	return convdomain.ConversationEntry{
		Message: sdk.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent("ok"),
			ToolCallID: &id,
		},
		Time: time.Now(),
		ToolExecution: &agentdomain.ToolExecutionResult{
			ToolName: tc.Function.Name,
			Success:  true,
		},
	}
}

func newApprovingCtx(
	tools []*sdk.ChatCompletionMessageToolCall,
	mode agentdomain.AgentMode,
	execStub func(sdk.ChatCompletionMessageToolCall, bool) convdomain.ConversationEntry,
	approveStub func(sdk.ChatCompletionMessageToolCall) (bool, string, error),
) (*states.StateContext, *[]convdomain.ConversationEntry, *[]sdk.Message, chan states.AgentEvent) {
	tna := []sdk.ChatCompletionMessageToolCall{}
	idx := 0
	tr := []convdomain.ConversationEntry{}
	conv := []sdk.Message{}
	events := make(chan states.AgentEvent, 16)
	wg := &sync.WaitGroup{}
	mu := &sync.Mutex{}

	ctx := &states.StateContext{
		Events:               events,
		WaitGroup:            wg,
		Mutex:                mu,
		CurrentToolCalls:     &tools,
		ToolsNeedingApproval: &tna,
		CurrentToolIndex:     &idx,
		ToolResults:          &tr,
		MaxConcurrentTools:   5,
		Request:              &agentdomain.AgentRequest{RequestID: "req-1"},
		AgentCtx: &states.AgentContext{
			Ctx:          context.Background(),
			Conversation: &conv,
		},
		RequestToolApproval: approveStub,
		ExecuteToolInternal: execStub,
		PublishChatEvent:    func(agentdomain.ChatEvent) {},
		AddMessage:          func(convdomain.ConversationEntry) error { return nil },
		GetAgentMode:        func() agentdomain.AgentMode { return mode },
	}
	return ctx, &tr, &conv, events
}

func waitForAllToolsProcessed(t *testing.T, events chan states.AgentEvent) {
	t.Helper()
	for {
		select {
		case evt := <-events:
			if _, ok := evt.(states.AllToolsProcessedEvent); ok {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for states.AllToolsProcessedEvent")
		}
	}
}

// TestApprovingToolsState_OverlapsExecution proves that approved tools execute
// concurrently while the remaining tools are still being approved. Each tool's
// execution blocks on a barrier until ALL tools have started executing; if the
// state serialized execution (running each approved tool to completion before
// requesting the next approval), only one tool would ever reach the barrier and
// states.AllToolsProcessedEvent would never arrive within the timeout.
func TestApprovingToolsState_OverlapsExecution(t *testing.T) {
	const n = 3
	arrivals := make(chan struct{}, n)
	allArrived := make(chan struct{})

	go func() {
		for i := 0; i < n; i++ {
			select {
			case <-arrivals:
			case <-time.After(2 * time.Second):
				return // never closes allArrived -> overlap did not happen
			}
		}
		close(allArrived)
	}()

	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) convdomain.ConversationEntry {
		arrivals <- struct{}{}
		select {
		case <-allArrived:
		case <-time.After(5 * time.Second): // safety so goroutines don't leak on failure
		}
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, string, error) { return true, "", nil }

	ctx, _, _, events := newApprovingCtx(makeTools(n), agentdomain.AgentModeStandard, execStub, approveStub)
	s := states.NewApprovingToolsState(ctx)

	require.NoError(t, s.Handle(states.MessageReceivedEvent{}))

	select {
	case evt := <-events:
		_, ok := evt.(states.AllToolsProcessedEvent)
		assert.True(t, ok, "expected states.AllToolsProcessedEvent, got %T", evt)
	case <-time.After(2 * time.Second):
		t.Fatal("approved tools did not execute concurrently - execution appears serialized")
	}
}

// TestApprovingToolsState_PreservesToolCallOrder verifies that even when tools
// finish out of order, results are appended to ToolResults and the conversation
// in tool-call order (required by the conversation validator).
func TestApprovingToolsState_PreservesToolCallOrder(t *testing.T) {
	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) convdomain.ConversationEntry {
		switch tc.ID {
		case "call-0":
			time.Sleep(60 * time.Millisecond) // finishes last
		case "call-1":
			time.Sleep(30 * time.Millisecond)
		}
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, string, error) { return true, "", nil }

	ctx, results, conv, events := newApprovingCtx(makeTools(3), agentdomain.AgentModeStandard, execStub, approveStub)
	s := states.NewApprovingToolsState(ctx)

	require.NoError(t, s.Handle(states.MessageReceivedEvent{}))
	waitForAllToolsProcessed(t, events)

	require.Len(t, *results, 3)
	require.Len(t, *conv, 3)
	for i := 0; i < 3; i++ {
		want := fmt.Sprintf("call-%d", i)
		require.NotNil(t, (*results)[i].Message.ToolCallID)
		assert.Equal(t, want, *(*results)[i].Message.ToolCallID, "ToolResults[%d] out of order", i)
		require.NotNil(t, (*conv)[i].ToolCallID)
		assert.Equal(t, want, *(*conv)[i].ToolCallID, "conversation[%d] out of order", i)
	}
}

// TestApprovingToolsState_FlushesResultsIncrementally proves a completed tool's
// result is written to the conversation/storage as soon as it (and the
// preceding results) finish, rather than being held until the whole batch
// completes. Tool 2 blocks; tools 0 and 1 must be flushed while tool 2 is still
// running - the previous batch-at-the-end behavior would time out here.
func TestApprovingToolsState_FlushesResultsIncrementally(t *testing.T) {
	block := make(chan struct{})
	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) convdomain.ConversationEntry {
		if tc.ID == "call-2" {
			<-block
		}
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, string, error) { return true, "", nil }

	ctx, _, _, events := newApprovingCtx(makeTools(3), agentdomain.AgentModeStandard, execStub, approveStub)

	added := make(chan string, 8)
	ctx.AddMessage = func(e convdomain.ConversationEntry) error {
		if e.Message.ToolCallID != nil {
			added <- *e.Message.ToolCallID
		}
		return nil
	}

	s := states.NewApprovingToolsState(ctx)
	require.NoError(t, s.Handle(states.MessageReceivedEvent{}))

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case id := <-added:
			got[id] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("results were not flushed incrementally (still batched); flushed so far: %v", got)
		}
	}
	assert.True(t, got["call-0"] && got["call-1"], "expected call-0 and call-1 flushed while call-2 still running, got %v", got)

	close(block)
	waitForAllToolsProcessed(t, events)
}

// TestApprovingToolsState_RejectionStopsTurn is a regression test for issue
// #786: rejecting a tool must end the turn instead of feeding the rejection
// back for another LLM turn. The rejection entry must carry
// ToolExecution.Rejected and HasToolResults must be cleared even when another
// tool in the batch was approved and executed.
func TestApprovingToolsState_RejectionStopsTurn(t *testing.T) {
	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) convdomain.ConversationEntry {
		return toolEntry(tc)
	}
	approveStub := func(tc sdk.ChatCompletionMessageToolCall) (bool, string, error) {
		return tc.ID != "call-0", "", nil
	}

	ctx, results, conv, events := newApprovingCtx(makeTools(2), agentdomain.AgentModeStandard, execStub, approveStub)
	s := states.NewApprovingToolsState(ctx)

	require.NoError(t, s.Handle(states.MessageReceivedEvent{}))
	waitForAllToolsProcessed(t, events)

	require.Len(t, *results, 2)
	require.Len(t, *conv, 2)

	rejection := (*results)[0]
	require.NotNil(t, rejection.ToolExecution)
	assert.True(t, rejection.ToolExecution.Rejected, "rejection entry must be marked Rejected")
	assert.False(t, rejection.ToolExecution.Success)
	require.NotNil(t, rejection.Message.ToolCallID)
	assert.Equal(t, "call-0", *rejection.Message.ToolCallID)

	assert.True(t, ctx.AgentCtx.LastToolFailed, "rejection counts as a failed tool")
	assert.False(t, ctx.AgentCtx.HasToolResults, "rejection must clear HasToolResults so the turn completes")
}

// TestApprovingToolsState_RejectionEntryKeepsArguments verifies the rejected
// tool entry carries the original call arguments so the UI renders
// "Bash(command=...)" instead of a bare "Bash()", and that a "failed" progress
// event is published so the queued preview line is dropped (issue #861).

// TestApprovingToolsState_ApprovedBatchKeepsToolResults verifies the inverse of
// the rejection case: a fully approved batch leaves HasToolResults set so the
// agent streams a follow-up turn responding to the results.
func TestApprovingToolsState_ApprovedBatchKeepsToolResults(t *testing.T) {
	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) convdomain.ConversationEntry {
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, string, error) { return true, "", nil }

	ctx, results, _, events := newApprovingCtx(makeTools(2), agentdomain.AgentModeStandard, execStub, approveStub)
	s := states.NewApprovingToolsState(ctx)

	require.NoError(t, s.Handle(states.MessageReceivedEvent{}))
	waitForAllToolsProcessed(t, events)

	require.Len(t, *results, 2)
	assert.True(t, ctx.AgentCtx.HasToolResults, "approved batch must keep HasToolResults for the follow-up turn")
	assert.False(t, ctx.AgentCtx.LastToolFailed)
}

// TestApprovingToolsState_AutoAcceptExecutesAll verifies that in auto-accept
// mode every tool is executed (concurrently) and results are collected in order.
func TestApprovingToolsState_AutoAcceptExecutesAll(t *testing.T) {
	var mu sync.Mutex
	executed := map[string]bool{}

	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) convdomain.ConversationEntry {
		mu.Lock()
		executed[tc.ID] = true
		mu.Unlock()
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, string, error) { return true, "", nil }

	ctx, results, _, events := newApprovingCtx(makeTools(3), agentdomain.AgentModeAutoAccept, execStub, approveStub)
	s := states.NewApprovingToolsState(ctx)

	require.NoError(t, s.Handle(states.MessageReceivedEvent{}))
	waitForAllToolsProcessed(t, events)

	require.Len(t, *results, 3)
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < 3; i++ {
		assert.True(t, executed[fmt.Sprintf("call-%d", i)], "call-%d was not executed", i)
	}
}
