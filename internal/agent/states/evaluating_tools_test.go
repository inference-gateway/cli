package states

import (
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestEvaluatingToolsState_ApprovalRouting covers how a batch that needs
// approval is routed by deliverability: promptable approvals go to
// ApprovingTools (dispatching the pre_tool hook), a batch whose every gated
// tool resolves to approval_behaviour=block goes to BlockingTools (no
// pre_tool hook), and a mixed batch still prompts.
func TestEvaluatingToolsState_ApprovalRouting(t *testing.T) {
	blockAll := func(*sdk.ChatCompletionMessageToolCall) string { return config.ApprovalBehaviourBlock }
	blockFirst := func(tc *sdk.ChatCompletionMessageToolCall) string {
		if tc.ID == "call-0" {
			return config.ApprovalBehaviourBlock
		}
		return config.ApprovalBehaviourPrompt
	}

	tests := []struct {
		name           string
		delivery       func(*sdk.ChatCompletionMessageToolCall) string
		wantTransition domain.AgentExecutionState
		wantHooks      []domain.HookPoint
	}{
		{
			name:           "deliverable approval routes to approving tools",
			wantTransition: domain.StateApprovingTools,
			wantHooks:      []domain.HookPoint{domain.HookPreTool},
		},
		{
			name:           "undeliverable approval for every gated tool routes to blocking tools",
			delivery:       blockAll,
			wantTransition: domain.StateBlockingTools,
		},
		{
			name:           "mixed deliverability still prompts",
			delivery:       blockFirst,
			wantTransition: domain.StateApprovingTools,
			wantHooks:      []domain.HookPoint{domain.HookPreTool},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			hooks := f.recordHooks()
			*f.ctx.CurrentToolCalls = makeTools(2)
			f.ctx.ShouldRequireApproval = func(*sdk.ChatCompletionMessageToolCall, bool) bool { return true }
			f.ctx.ApprovalDelivery = tt.delivery
			s := NewEvaluatingToolsState(f.ctx)
			assert.Equal(t, domain.StateEvaluatingTools, s.Name())

			require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))

			assertTransitions(t, f.sm, tt.wantTransition)
			assertEvents(t, f.events, domain.MessageReceivedEvent{})
			assert.Equal(t, tt.wantHooks, *hooks)
			require.Len(t, f.completeCalls, 1, "tool calls must be published before evaluation")
			assert.Len(t, f.completeCalls[0].toolCalls, 2)
		})
	}
}

// TestEvaluatingToolsState_NoApprovalSpawnsExecutor verifies a batch with no
// gated tools transitions to ExecutingTools and launches the tool executor on
// a background goroutine (which owns the WaitGroup.Done, mirroring
// EventDrivenAgent.executeTools).
func TestEvaluatingToolsState_NoApprovalSpawnsExecutor(t *testing.T) {
	f := newStateFixture()
	hooks := f.recordHooks()
	*f.ctx.CurrentToolCalls = makeTools(2)
	ran := make(chan struct{})
	executor := func() {
		defer f.ctx.WaitGroup.Done()
		close(ran)
	}
	f.ctx.ToolExecutor = &executor
	s := NewEvaluatingToolsState(f.ctx)

	require.NoError(t, s.Handle(domain.MessageReceivedEvent{}))

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("tool executor was not invoked")
	}
	f.ctx.WaitGroup.Wait()

	assertTransitions(t, f.sm, domain.StateExecutingTools)
	assertEvents(t, f.events)
	assert.Equal(t, []domain.HookPoint{domain.HookPreTool}, *hooks)
}

// TestEvaluatingToolsState_TransitionFailureIsReturned verifies a failed
// transition surfaces the error and the executor is never launched.
func TestEvaluatingToolsState_TransitionFailureIsReturned(t *testing.T) {
	f := newStateFixture()
	*f.ctx.CurrentToolCalls = makeTools(1)
	f.sm.TransitionReturns(errBoom)
	executorRan := false
	executor := func() { executorRan = true; f.ctx.WaitGroup.Done() }
	f.ctx.ToolExecutor = &executor
	s := NewEvaluatingToolsState(f.ctx)

	err := s.Handle(domain.MessageReceivedEvent{})

	assert.ErrorIs(t, err, errBoom)
	assertEvents(t, f.events)
	assert.False(t, executorRan, "executor must not run when the transition fails")
}
