package states

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	"testing"

	assert "github.com/stretchr/testify/assert"
)

// TestCheckingQueueState_Handle covers the routing priorities of the
// CheckingQueue executor: pending tool results stream first (without touching
// the queue), queued messages are drained with the drain hooks, a cancelled
// session short-circuits to Completing, met completion conditions go to
// Completing, and otherwise the loop continues with a new stream.
func TestCheckingQueueState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		event           AgentEvent
		setup           func(f *stateFixture)
		transitionErr   error
		wantErr         bool
		wantTransitions []AgentExecutionState
		wantEvents      []AgentEvent
		wantHooks       []agentdomain.HookPoint
		wantDrainCalls  int
	}{
		{
			name:            "tool results pending stream first without draining",
			event:           MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.ctx.AgentCtx.HasToolResults = true },
			wantTransitions: []AgentExecutionState{StateStreamingLLM},
			wantEvents:      []AgentEvent{StartStreamingEvent{}},
		},
		{
			name:  "queued messages are drained with both drain hooks",
			event: MessageReceivedEvent{},
			setup: func(f *stateFixture) {
				f.queue.IsEmptyReturns(false)
				f.drainReturns = 2
			},
			wantTransitions: []AgentExecutionState{StateStreamingLLM},
			wantEvents:      []AgentEvent{StartStreamingEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPreQueueDrain, agentdomain.HookPostQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name:            "drain of zero messages skips the post-drain hook",
			event:           MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.queue.IsEmptyReturns(false) },
			wantTransitions: []AgentExecutionState{StateStreamingLLM},
			wantEvents:      []AgentEvent{StartStreamingEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPreQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name:            "cancelled session completes without another turn",
			event:           MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.cancelSession() },
			wantTransitions: []AgentExecutionState{StateCompleting},
			wantEvents:      []AgentEvent{CompletionRequestedEvent{}},
		},
		{
			name:            "completion conditions met",
			event:           MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.sm.CanTransitionReturns(true) },
			wantTransitions: []AgentExecutionState{StateCompleting},
			wantEvents:      []AgentEvent{CompletionRequestedEvent{}},
		},
		{
			name:            "default continues the loop with a new stream",
			event:           MessageReceivedEvent{},
			wantTransitions: []AgentExecutionState{StateStreamingLLM},
			wantEvents:      []AgentEvent{StartStreamingEvent{}},
		},
		{
			name:            "transition failure is returned",
			event:           MessageReceivedEvent{},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []AgentExecutionState{StateStreamingLLM},
		},
		{
			name:  "stray event is a no-op",
			event: CompletionRequestedEvent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			hooks := f.recordHooks()
			if tt.setup != nil {
				tt.setup(f)
			}
			f.sm.TransitionReturns(tt.transitionErr)
			s := NewCheckingQueueState(f.ctx)
			assert.Equal(t, StateCheckingQueue, s.Name())

			err := s.Handle(tt.event)

			if tt.wantErr {
				assert.ErrorIs(t, err, errBoom)
			} else {
				assert.NoError(t, err)
			}
			assertTransitions(t, f.sm, tt.wantTransitions...)
			assertEvents(t, f.events, tt.wantEvents...)
			assert.Equal(t, tt.wantHooks, *hooks)
			assert.Equal(t, tt.wantDrainCalls, f.drainCalls, "BatchDrainQueue calls")
		})
	}
}
