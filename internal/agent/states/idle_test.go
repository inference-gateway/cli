package states_test

import (
	states "github.com/inference-gateway/cli/internal/agent/states"
	"testing"

	assert "github.com/stretchr/testify/assert"
)

// TestIdleState_Handle drives the Idle executor through its three paths: a
// states.MessageReceivedEvent starts the loop (transition to CheckingQueue and
// re-emit the event there), a failed transition surfaces the error without
// emitting, and any other event is ignored.
func TestIdleState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		event           states.AgentEvent
		transitionErr   error
		wantErr         bool
		wantTransitions []states.AgentExecutionState
		wantEvents      []states.AgentEvent
	}{
		{
			name:            "message received starts the loop",
			event:           states.MessageReceivedEvent{},
			wantTransitions: []states.AgentExecutionState{states.StateCheckingQueue},
			wantEvents:      []states.AgentEvent{states.MessageReceivedEvent{}},
		},
		{
			name:            "transition failure is returned and nothing is emitted",
			event:           states.MessageReceivedEvent{},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []states.AgentExecutionState{states.StateCheckingQueue},
		},
		{
			name:  "stray event is a no-op",
			event: states.CompletionRequestedEvent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			f.sm.TransitionReturns(tt.transitionErr)
			s := states.NewIdleState(f.ctx)
			assert.Equal(t, states.StateIdle, s.Name())

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
