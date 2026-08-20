package states_test

import (
	states "github.com/inference-gateway/cli/internal/agent/states"
	"testing"

	assert "github.com/stretchr/testify/assert"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TestCheckingQueueState_Handle covers the routing priorities of the
// CheckingQueue executor: pending tool results stream first (without touching
// the queue), queued messages are drained with the drain hooks, a cancelled
// session short-circuits to Completing, met completion conditions go to
// Completing, and otherwise the loop continues with a new stream.
func TestCheckingQueueState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		event           states.AgentEvent
		setup           func(f *stateFixture)
		transitionErr   error
		wantErr         bool
		wantTransitions []states.AgentExecutionState
		wantEvents      []states.AgentEvent
		wantHooks       []agentdomain.HookPoint
		wantDrainCalls  int
	}{
		{
			name:            "tool results pending stream first without draining",
			event:           states.MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.ctx.AgentCtx.HasToolResults = true },
			wantTransitions: []states.AgentExecutionState{states.StateStreamingLLM},
			wantEvents:      []states.AgentEvent{states.StartStreamingEvent{}},
		},
		{
			name:  "queued messages are drained with both drain hooks",
			event: states.MessageReceivedEvent{},
			setup: func(f *stateFixture) {
				f.queue.IsEmptyReturns(false)
				f.drainReturns = 2
			},
			wantTransitions: []states.AgentExecutionState{states.StateStreamingLLM},
			wantEvents:      []states.AgentEvent{states.StartStreamingEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPreQueueDrain, agentdomain.HookPostQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name:            "drain of zero messages skips the post-drain hook",
			event:           states.MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.queue.IsEmptyReturns(false) },
			wantTransitions: []states.AgentExecutionState{states.StateStreamingLLM},
			wantEvents:      []states.AgentEvent{states.StartStreamingEvent{}},
			wantHooks:       []agentdomain.HookPoint{agentdomain.HookPreQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name:            "cancelled session completes without another turn",
			event:           states.MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.cancelSession() },
			wantTransitions: []states.AgentExecutionState{states.StateCompleting},
			wantEvents:      []states.AgentEvent{states.CompletionRequestedEvent{}},
		},
		{
			name:            "completion conditions met",
			event:           states.MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.sm.CanTransitionReturns(true) },
			wantTransitions: []states.AgentExecutionState{states.StateCompleting},
			wantEvents:      []states.AgentEvent{states.CompletionRequestedEvent{}},
		},
		{
			name:            "default continues the loop with a new stream",
			event:           states.MessageReceivedEvent{},
			wantTransitions: []states.AgentExecutionState{states.StateStreamingLLM},
			wantEvents:      []states.AgentEvent{states.StartStreamingEvent{}},
		},
		{
			name:            "transition failure is returned",
			event:           states.MessageReceivedEvent{},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []states.AgentExecutionState{states.StateStreamingLLM},
		},
		{
			name:  "stray event is a no-op",
			event: states.CompletionRequestedEvent{},
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
			s := states.NewCheckingQueueState(f.ctx)
			assert.Equal(t, states.StateCheckingQueue, s.Name())

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
