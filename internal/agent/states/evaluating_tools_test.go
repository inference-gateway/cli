package states_test

import (
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	states "github.com/inference-gateway/cli/internal/agent/states"
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
		wantTransition states.AgentExecutionState
		wantHooks      []agentdomain.HookPoint
	}{
		{
			name:           "deliverable approval routes to approving tools",
			wantTransition: states.StateApprovingTools,
			wantHooks:      []agentdomain.HookPoint{agentdomain.HookPreTool},
		},
		{
			name:           "undeliverable approval for every gated tool routes to blocking tools",
			delivery:       blockAll,
			wantTransition: states.StateBlockingTools,
		},
		{
			name:           "mixed deliverability still prompts",
			delivery:       blockFirst,
			wantTransition: states.StateApprovingTools,
			wantHooks:      []agentdomain.HookPoint{agentdomain.HookPreTool},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			hooks := f.recordHooks()
			*f.ctx.CurrentToolCalls = makeTools(2)
			f.ctx.ShouldRequireApproval = func(*sdk.ChatCompletionMessageToolCall, bool) bool { return true }
			f.ctx.ApprovalDelivery = tt.delivery
			s := states.NewEvaluatingToolsState(f.ctx)
			assert.Equal(t, states.StateEvaluatingTools, s.Name())

			require.NoError(t, s.Handle(states.MessageReceivedEvent{}))

			assertTransitions(t, f.sm, tt.wantTransition)
			assertEvents(t, f.events, states.MessageReceivedEvent{})
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
	s := states.NewEvaluatingToolsState(f.ctx)

	require.NoError(t, s.Handle(states.MessageReceivedEvent{}))

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("tool executor was not invoked")
	}
	f.ctx.WaitGroup.Wait()

	assertTransitions(t, f.sm, states.StateExecutingTools)
	assertEvents(t, f.events)
	assert.Equal(t, []agentdomain.HookPoint{agentdomain.HookPreTool}, *hooks)
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
	s := states.NewEvaluatingToolsState(f.ctx)

	err := s.Handle(states.MessageReceivedEvent{})

	assert.ErrorIs(t, err, errBoom)
	assertEvents(t, f.events)
	assert.False(t, executorRan, "executor must not run when the transition fails")
}
