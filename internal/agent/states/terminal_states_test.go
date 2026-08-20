package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
)

// TestTerminalStates_IgnoreAllEvents verifies the three terminal states
// (Cancelled, Error, Stopped) report the right name and treat every event as
// a no-op: no error, no transition, nothing emitted — so the event loop can
// exit cleanly once one of them is reached.
func TestTerminalStates_IgnoreAllEvents(t *testing.T) {
	tests := []struct {
		name  string
		build func(ctx *StateContext) StateHandler
		want  AgentExecutionState
	}{
		{"cancelled", NewCancelledState, StateCancelled},
		{"error", NewErrorState, StateError},
		{"stopped", NewStoppedState, StateStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			s := tt.build(f.ctx)
			assert.Equal(t, tt.want, s.Name())

			for _, evt := range []AgentEvent{
				MessageReceivedEvent{},
				CompletionRequestedEvent{},
				AllToolsProcessedEvent{},
				ToolsCompletedEvent{},
			} {
				assert.NoError(t, s.Handle(evt), "event %T", evt)
			}

			assertTransitions(t, f.sm)
			assertEvents(t, f.events)
		})
	}
}
