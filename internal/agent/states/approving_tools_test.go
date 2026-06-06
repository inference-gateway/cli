package states

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
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

func toolEntry(tc sdk.ChatCompletionMessageToolCall) domain.ConversationEntry {
	id := tc.ID
	return domain.ConversationEntry{
		Message: sdk.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent("ok"),
			ToolCallID: &id,
		},
		Time: time.Now(),
		ToolExecution: &domain.ToolExecutionResult{
			ToolName: tc.Function.Name,
			Success:  true,
		},
	}
}

func newApprovingCtx(
	tools []*sdk.ChatCompletionMessageToolCall,
	mode domain.AgentMode,
	execStub func(sdk.ChatCompletionMessageToolCall, bool) domain.ConversationEntry,
	approveStub func(sdk.ChatCompletionMessageToolCall) (bool, error),
) (*domain.StateContext, *[]domain.ConversationEntry, *[]sdk.Message, chan domain.AgentEvent) {
	tna := []sdk.ChatCompletionMessageToolCall{}
	idx := 0
	tr := []domain.ConversationEntry{}
	conv := []sdk.Message{}
	events := make(chan domain.AgentEvent, 16)
	wg := &sync.WaitGroup{}
	mu := &sync.Mutex{}

	ctx := &domain.StateContext{
		Events:               events,
		WaitGroup:            wg,
		Mutex:                mu,
		CurrentToolCalls:     &tools,
		ToolsNeedingApproval: &tna,
		CurrentToolIndex:     &idx,
		ToolResults:          &tr,
		MaxConcurrentTools:   5,
		Request:              &domain.AgentRequest{RequestID: "req-1"},
		AgentCtx: &domain.AgentContext{
			Ctx:          context.Background(),
			Conversation: &conv,
		},
		RequestToolApproval: approveStub,
		ExecuteToolInternal: execStub,
		PublishChatEvent:    func(domain.ChatEvent) {},
		AddMessage:          func(domain.ConversationEntry) error { return nil },
		GetAgentMode:        func() domain.AgentMode { return mode },
	}
	return ctx, &tr, &conv, events
}

func waitForAllToolsProcessed(t *testing.T, events chan domain.AgentEvent) {
	t.Helper()
	for {
		select {
		case evt := <-events:
			if _, ok := evt.(domain.AllToolsProcessedEvent); ok {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for AllToolsProcessedEvent")
		}
	}
}

// TestApprovingToolsState_OverlapsExecution proves that approved tools execute
// concurrently while the remaining tools are still being approved. Each tool's
// execution blocks on a barrier until ALL tools have started executing; if the
// state serialized execution (running each approved tool to completion before
// requesting the next approval), only one tool would ever reach the barrier and
// AllToolsProcessedEvent would never arrive within the timeout.
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

	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) domain.ConversationEntry {
		arrivals <- struct{}{}
		select {
		case <-allArrived:
		case <-time.After(5 * time.Second): // safety so goroutines don't leak on failure
		}
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, error) { return true, nil }

	ctx, _, _, events := newApprovingCtx(makeTools(n), domain.AgentModeStandard, execStub, approveStub)
	s := &ApprovingToolsState{ctx: ctx}

	require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))

	select {
	case evt := <-events:
		_, ok := evt.(domain.AllToolsProcessedEvent)
		assert.True(t, ok, "expected AllToolsProcessedEvent, got %T", evt)
	case <-time.After(2 * time.Second):
		t.Fatal("approved tools did not execute concurrently — execution appears serialized")
	}
}

// TestApprovingToolsState_PreservesToolCallOrder verifies that even when tools
// finish out of order, results are appended to ToolResults and the conversation
// in tool-call order (required by the conversation validator).
func TestApprovingToolsState_PreservesToolCallOrder(t *testing.T) {
	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) domain.ConversationEntry {
		switch tc.ID {
		case "call-0":
			time.Sleep(60 * time.Millisecond) // finishes last
		case "call-1":
			time.Sleep(30 * time.Millisecond)
		}
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, error) { return true, nil }

	ctx, results, conv, events := newApprovingCtx(makeTools(3), domain.AgentModeStandard, execStub, approveStub)
	s := &ApprovingToolsState{ctx: ctx}

	require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))
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
// running — the previous batch-at-the-end behavior would time out here.
func TestApprovingToolsState_FlushesResultsIncrementally(t *testing.T) {
	block := make(chan struct{})
	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) domain.ConversationEntry {
		if tc.ID == "call-2" {
			<-block
		}
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, error) { return true, nil }

	ctx, _, _, events := newApprovingCtx(makeTools(3), domain.AgentModeStandard, execStub, approveStub)

	added := make(chan string, 8)
	ctx.AddMessage = func(e domain.ConversationEntry) error {
		if e.Message.ToolCallID != nil {
			added <- *e.Message.ToolCallID
		}
		return nil
	}

	s := &ApprovingToolsState{ctx: ctx}
	require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))

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

// TestApprovingToolsState_AutoAcceptExecutesAll verifies that in auto-accept
// mode every tool is executed (concurrently) and results are collected in order.
func TestApprovingToolsState_AutoAcceptExecutesAll(t *testing.T) {
	var mu sync.Mutex
	executed := map[string]bool{}

	execStub := func(tc sdk.ChatCompletionMessageToolCall, _ bool) domain.ConversationEntry {
		mu.Lock()
		executed[tc.ID] = true
		mu.Unlock()
		return toolEntry(tc)
	}
	approveStub := func(sdk.ChatCompletionMessageToolCall) (bool, error) { return true, nil }

	ctx, results, _, events := newApprovingCtx(makeTools(3), domain.AgentModeAutoAccept, execStub, approveStub)
	s := &ApprovingToolsState{ctx: ctx}

	require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))
	waitForAllToolsProcessed(t, events)

	require.Len(t, *results, 3)
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < 3; i++ {
		assert.True(t, executed[fmt.Sprintf("call-%d", i)], "call-%d was not executed", i)
	}
}
