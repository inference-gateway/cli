package states

import (
	"fmt"
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func requireAllApproval(*sdk.ChatCompletionMessageToolCall, bool) bool { return true }

// TestBlockingToolsState_GatedToolsAreRejected verifies that with no approver
// reachable every gated tool is turned into a rejected "Blocked:" tool result
// (appended to ToolResults, the conversation, and storage in tool-call order)
// and the batch ends with LastToolFailed set and AllToolsProcessedEvent.
func TestBlockingToolsState_GatedToolsAreRejected(t *testing.T) {
	f := newStateFixture()
	*f.ctx.CurrentToolCalls = makeTools(2)
	f.ctx.ShouldRequireApproval = requireAllApproval
	s := NewBlockingToolsState(f.ctx)
	assert.Equal(t, domain.StateBlockingTools, s.Name())

	require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))
	waitForAllToolsProcessed(t, f.events)
	f.ctx.WaitGroup.Wait()

	results := *f.ctx.ToolResults
	require.Len(t, results, 2)
	for i, entry := range results {
		require.NotNil(t, entry.ToolExecution, "result %d", i)
		assert.True(t, entry.ToolExecution.Rejected, "result %d must be rejected", i)
		assert.False(t, entry.ToolExecution.Success, "result %d must not succeed", i)
		content, _ := entry.Message.Content.AsMessageContent0()
		assert.True(t, strings.HasPrefix(content, "Blocked:"), "result %d content: %q", i, content)
		require.NotNil(t, entry.Message.ToolCallID)
		assert.Equal(t, fmt.Sprintf("call-%d", i), *entry.Message.ToolCallID, "result %d out of order", i)
	}
	assert.Len(t, *f.ctx.AgentCtx.Conversation, 2)
	assert.Len(t, f.added, 2, "each result must be persisted via AddMessage")
	assert.True(t, f.ctx.AgentCtx.LastToolFailed)
	assert.True(t, f.ctx.AgentCtx.HasToolResults)
}

// TestBlockingToolsState_MixedBatchRunsNonGatedTools verifies tools that do
// not require approval still execute in a blocked batch, preserving tool-call
// order.
func TestBlockingToolsState_MixedBatchRunsNonGatedTools(t *testing.T) {
	f := newStateFixture()
	*f.ctx.CurrentToolCalls = makeTools(2)
	f.ctx.ShouldRequireApproval = func(tc *sdk.ChatCompletionMessageToolCall, _ bool) bool {
		return tc.ID == "call-0"
	}
	s := NewBlockingToolsState(f.ctx)

	require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))
	waitForAllToolsProcessed(t, f.events)
	f.ctx.WaitGroup.Wait()

	results := *f.ctx.ToolResults
	require.Len(t, results, 2)

	blocked, executed := results[0], results[1]
	require.NotNil(t, blocked.ToolExecution)
	assert.True(t, blocked.ToolExecution.Rejected)
	require.NotNil(t, blocked.Message.ToolCallID)
	assert.Equal(t, "call-0", *blocked.Message.ToolCallID)

	require.NotNil(t, executed.ToolExecution)
	assert.True(t, executed.ToolExecution.Success, "non-gated tool must run normally")
	assert.False(t, executed.ToolExecution.Rejected)
	require.NotNil(t, executed.Message.ToolCallID)
	assert.Equal(t, "call-1", *executed.Message.ToolCallID)

	assert.True(t, f.ctx.AgentCtx.LastToolFailed, "the blocked tool counts as failed")
}

// TestBlockingToolsState_CancelledSessionSkipsBatch verifies a cancelled
// session context skips the whole batch but still emits
// AllToolsProcessedEvent so the loop can wind down.
func TestBlockingToolsState_CancelledSessionSkipsBatch(t *testing.T) {
	f := newStateFixture()
	*f.ctx.CurrentToolCalls = makeTools(2)
	f.ctx.ShouldRequireApproval = requireAllApproval
	f.cancelSession()
	s := NewBlockingToolsState(f.ctx)

	require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))
	waitForAllToolsProcessed(t, f.events)
	f.ctx.WaitGroup.Wait()

	assert.Empty(t, *f.ctx.ToolResults)
	assert.False(t, f.ctx.AgentCtx.LastToolFailed)
}

// TestBlockingToolsState_AllProcessedRoutesToPostToolExecution verifies the
// AllToolsProcessedEvent leg: transition to PostToolExecution and re-emit, or
// surface a failed transition.
func TestBlockingToolsState_AllProcessedRoutesToPostToolExecution(t *testing.T) {
	tests := []struct {
		name          string
		transitionErr error
		wantErr       bool
		wantEvents    []domain.AgentEvent
	}{
		{
			name:       "advances to post tool execution",
			wantEvents: []domain.AgentEvent{domain.MessageReceivedEvent{}},
		},
		{
			name:          "transition failure is returned",
			transitionErr: errBoom,
			wantErr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			f.sm.TransitionReturns(tt.transitionErr)
			s := NewBlockingToolsState(f.ctx)

			err := s.Handle(domain.AllToolsProcessedEvent{})

			if tt.wantErr {
				assert.ErrorIs(t, err, errBoom)
			} else {
				assert.NoError(t, err)
			}
			assertTransitions(t, f.sm, domain.StatePostToolExecution)
			assertEvents(t, f.events, tt.wantEvents...)
		})
	}
}
