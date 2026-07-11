package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestIdleState_Handle drives the Idle executor through its three paths: a
// MessageReceivedEvent starts the loop (transition to CheckingQueue and
// re-emit the event there), a failed transition surfaces the error without
// emitting, and any other event is ignored.
func TestIdleState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		event           domain.AgentEvent
		transitionErr   error
		wantErr         bool
		wantTransitions []domain.AgentExecutionState
		wantEvents      []domain.AgentEvent
	}{
		{
			name:            "message received starts the loop",
			event:           domain.MessageReceivedEvent{},
			wantTransitions: []domain.AgentExecutionState{domain.StateCheckingQueue},
			wantEvents:      []domain.AgentEvent{domain.MessageReceivedEvent{}},
		},
		{
			name:            "transition failure is returned and nothing is emitted",
			event:           domain.MessageReceivedEvent{},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []domain.AgentExecutionState{domain.StateCheckingQueue},
		},
		{
			name:  "stray event is a no-op",
			event: domain.CompletionRequestedEvent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			f.sm.TransitionReturns(tt.transitionErr)
			s := NewIdleState(f.ctx)
			assert.Equal(t, domain.StateIdle, s.Name())

			err := s.Handle(tt.event)

			if tt.wantErr {
				assert.ErrorIs(t, err, errBoom)
			} else {
				assert.NoError(t, err)
			}
			assertTransitions(t, f.sm, tt.wantTransitions...)
			assertEvents(t, f.events, tt.wantEvents...)
		})
	}
}
