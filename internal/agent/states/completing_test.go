package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestCompletingState_IgnoresNonCompletionEvents verifies that the Completing
// state finalizes only on CompletionRequestedEvent. A stray event (e.g. a
// MessageReceivedEvent) must be a no-op so it cannot trigger completion or
// enqueue any follow-up event.
func TestCompletingState_IgnoresNonCompletionEvents(t *testing.T) {
	events := make(chan domain.AgentEvent, 1)
	s := NewCompletingState(&domain.StateContext{Events: events})

	err := s.Handle(domain.MessageReceivedEvent{})

	assert.NoError(t, err)
	assert.Empty(t, events, "non-completion event must not enqueue any follow-up event")
}

// TestCompletingState_Complete covers the finalization paths: an empty queue
// publishes the final completion (with the post_session hook) and returns to
// Idle, messages queued during completion restart the loop instead, a
// cancelled session publishes cancellation rather than completion, and a
// failed transition surfaces the error.
func TestCompletingState_Complete(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(f *stateFixture)
		transitionErr   error
		wantErr         bool
		wantTransitions []domain.AgentExecutionState
		wantEvents      []domain.AgentEvent
		wantHooks       []domain.HookPoint
		wantComplete    int
		wantCancelled   int
	}{
		{
			name:            "empty queue publishes completion and returns to idle",
			wantTransitions: []domain.AgentExecutionState{domain.StateIdle},
			wantHooks:       []domain.HookPoint{domain.HookPostSession},
			wantComplete:    1,
		},
		{
			name:            "messages queued during completion restart the loop",
			setup:           func(f *stateFixture) { f.queue.IsEmptyReturns(false) },
			wantTransitions: []domain.AgentExecutionState{domain.StateCheckingQueue},
			wantEvents:      []domain.AgentEvent{domain.MessageReceivedEvent{}},
		},
		{
			name:            "cancelled session publishes cancellation instead of completion",
			setup:           func(f *stateFixture) { f.cancelSession() },
			wantTransitions: []domain.AgentExecutionState{domain.StateIdle},
			wantCancelled:   1,
		},
		{
			name:            "transition failure is returned",
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []domain.AgentExecutionState{domain.StateIdle},
			wantHooks:       []domain.HookPoint{domain.HookPostSession},
			wantComplete:    1,
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
			s := NewCompletingState(f.ctx)
			assert.Equal(t, domain.StateCompleting, s.Name())

			err := s.Handle(domain.CompletionRequestedEvent{})

			if tt.wantErr {
				assert.ErrorIs(t, err, errBoom)
			} else {
				assert.NoError(t, err)
			}
			assertTransitions(t, f.sm, tt.wantTransitions...)
			assertEvents(t, f.events, tt.wantEvents...)
			assert.Equal(t, tt.wantHooks, *hooks)
			assert.Len(t, f.completeCalls, tt.wantComplete, "PublishChatComplete calls")
			assert.Equal(t, tt.wantCancelled, f.cancelCalls, "PublishChatCancelled calls")
		})
	}
}
