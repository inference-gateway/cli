package states_test

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	states "github.com/inference-gateway/cli/internal/agent/loop/states"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// TestPostToolExecutionState_Handle covers the routing after a completed tool
// batch: tool images, a requested plan, queue drain, completion, and the
// cancelled-session "drain then stop" path - with the post_tool hook firing on
// every path but the plan.
func TestPostToolExecutionState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(f *stateFixture)
		transitionErr   error
		wantErr         bool
		wantTransitions []states.AgentExecutionState
		wantEvents      []states.AgentEvent
		wantHooks       []agentdomain.HookPoint
		wantDrainCalls  int
		wantImageCalls  int
	}{
		{
			name: "requested plan stops before hooks, drain and the next turn",
			setup: func(f *stateFixture) {
				f.ctx.AgentCtx.HasToolResults = true
				f.queue.IsEmptyReturns(false)
				f.ctx.PresentPlan = func([]convdomain.ConversationEntry) bool { return true }
			},
			wantTransitions: []states.AgentExecutionState{states.StateStopped},
			wantImageCalls:  1,
		},
		{
			name:            "results the model reads next carry their images",
			setup:           func(f *stateFixture) { f.ctx.AgentCtx.HasToolResults = true },
			wantTransitions: []states.AgentExecutionState{states.StateCheckingQueue},
			wantEvents:      []states.AgentEvent{states.MessageReceivedEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPostTool},
			wantImageCalls:  1,
		},
		{
			name: "no plan falls through to the normal routing",
			setup: func(f *stateFixture) {
				f.sm.CanTransitionReturns(true)
				f.ctx.PresentPlan = func([]convdomain.ConversationEntry) bool { return false }
			},
			wantTransitions: []states.AgentExecutionState{states.StateCompleting},
			wantEvents:      []states.AgentEvent{states.CompletionRequestedEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPostTool},
		},
		{
			name:            "rejected round completes without its images",
			setup:           func(f *stateFixture) { f.sm.CanTransitionReturns(true) },
			wantTransitions: []states.AgentExecutionState{states.StateCompleting},
			wantEvents:      []states.AgentEvent{states.CompletionRequestedEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPostTool},
		},
		{
			name:            "queue empty and cannot complete continues to next turn",
			wantTransitions: []states.AgentExecutionState{states.StateCheckingQueue},
			wantEvents:      []states.AgentEvent{states.MessageReceivedEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPostTool},
		},
		{
			name: "queued messages are drained before the next turn",
			setup: func(f *stateFixture) {
				f.queue.IsEmptyReturns(false)
				f.drainReturns = 1
			},
			wantTransitions: []states.AgentExecutionState{states.StateCheckingQueue},
			wantEvents:      []states.AgentEvent{states.MessageReceivedEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPostTool, agentdomain.HookPreQueueDrain, agentdomain.HookPostQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name: "cancelled session drains then completes without another turn",
			setup: func(f *stateFixture) {
				f.queue.IsEmptyReturns(false)
				f.drainReturns = 1
				f.cancelSession()
			},
			wantTransitions: []states.AgentExecutionState{states.StateCompleting},
			wantEvents:      []states.AgentEvent{states.CompletionRequestedEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPostTool, agentdomain.HookPreQueueDrain, agentdomain.HookPostQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name:            "transition failure is returned",
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []states.AgentExecutionState{states.StateCheckingQueue},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPostTool},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			hooks := f.recordHooks()
			imageCalls := 0
			f.ctx.AttachToolImages = func([]convdomain.ConversationEntry) { imageCalls++ }
			if tt.setup != nil {
				tt.setup(f)
			}
			f.sm.TransitionReturns(tt.transitionErr)
			s := states.NewPostToolExecutionState(f.ctx)
			assert.Equal(t, states.StatePostToolExecution, s.Name())

			err := s.Handle(states.MessageReceivedEvent{})

			if tt.wantErr {
				assert.ErrorIs(t, err, errBoom)
			} else {
				assert.NoError(t, err)
			}
			assertTransitions(t, f.sm, tt.wantTransitions...)
			assertEvents(t, f.events, tt.wantEvents...)
			assert.Equal(t, tt.wantHooks, *hooks)
			assert.Equal(t, tt.wantDrainCalls, f.drainCalls, "BatchDrainQueue calls")
			assert.Equal(t, tt.wantImageCalls, imageCalls, "AttachToolImages calls")
		})
	}
}
