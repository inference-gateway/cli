package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestCheckingQueueState_Handle covers the routing priorities of the
// CheckingQueue executor: pending tool results stream first (without touching
// the queue), queued messages are drained with the drain hooks, a cancelled
// session short-circuits to Completing, met completion conditions go to
// Completing, and otherwise the loop continues with a new stream.
func TestCheckingQueueState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		event           domain.AgentEvent
		setup           func(f *stateFixture)
		transitionErr   error
		wantErr         bool
		wantTransitions []domain.AgentExecutionState
		wantEvents      []domain.AgentEvent
		wantHooks       []domain.HookPoint
		wantDrainCalls  int
	}{
		{
			name:            "tool results pending stream first without draining",
			event:           domain.MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.ctx.AgentCtx.HasToolResults = true },
			wantTransitions: []domain.AgentExecutionState{domain.StateStreamingLLM},
			wantEvents:      []domain.AgentEvent{domain.StartStreamingEvent{}},
		},
		{
			name:  "queued messages are drained with both drain hooks",
			event: domain.MessageReceivedEvent{},
			setup: func(f *stateFixture) {
				f.queue.IsEmptyReturns(false)
				f.drainReturns = 2
			},
			wantTransitions: []domain.AgentExecutionState{domain.StateStreamingLLM},
			wantEvents:      []domain.AgentEvent{domain.StartStreamingEvent{}},
			wantHooks:       []domain.HookPoint{domain.HookPreQueueDrain, domain.HookPostQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name:            "drain of zero messages skips the post-drain hook",
			event:           domain.MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.queue.IsEmptyReturns(false) },
			wantTransitions: []domain.AgentExecutionState{domain.StateStreamingLLM},
			wantEvents:      []domain.AgentEvent{domain.StartStreamingEvent{}},
			wantHooks:       []domain.HookPoint{domain.HookPreQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name:            "cancelled session completes without another turn",
			event:           domain.MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.cancelSession() },
			wantTransitions: []domain.AgentExecutionState{domain.StateCompleting},
			wantEvents:      []domain.AgentEvent{domain.CompletionRequestedEvent{}},
		},
		{
			name:            "completion conditions met",
			event:           domain.MessageReceivedEvent{},
			setup:           func(f *stateFixture) { f.sm.CanTransitionReturns(true) },
			wantTransitions: []domain.AgentExecutionState{domain.StateCompleting},
			wantEvents:      []domain.AgentEvent{domain.CompletionRequestedEvent{}},
		},
		{
			name:            "default continues the loop with a new stream",
			event:           domain.MessageReceivedEvent{},
			wantTransitions: []domain.AgentExecutionState{domain.StateStreamingLLM},
			wantEvents:      []domain.AgentEvent{domain.StartStreamingEvent{}},
		},
		{
			name:            "transition failure is returned",
			event:           domain.MessageReceivedEvent{},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []domain.AgentExecutionState{domain.StateStreamingLLM},
		},
		{
			name:  "stray event is a no-op",
			event: domain.CompletionRequestedEvent{},
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
			assert.Equal(t, domain.StateCheckingQueue, s.Name())

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
