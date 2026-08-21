package states_test

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	states "github.com/inference-gateway/cli/internal/agent/states"
)

// TestTerminalStates_IgnoreAllEvents verifies the three terminal states
// (Cancelled, Error, Stopped) report the right name and treat every event as
// a no-op: no error, no transition, nothing emitted — so the event loop can
// exit cleanly once one of them is reached.
func TestTerminalStates_IgnoreAllEvents(t *testing.T) {
	tests := []struct {
		name  string
		build func(ctx *states.StateContext) states.StateHandler
		want  states.AgentExecutionState
	}{
		{"cancelled", states.NewCancelledState, states.StateCancelled},
		{"error", states.NewErrorState, states.StateError},
		{"stopped", states.NewStoppedState, states.StateStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			s := tt.build(f.ctx)
			assert.Equal(t, tt.want, s.Name())

			for _, evt := range []states.AgentEvent{
				states.MessageReceivedEvent{},
				states.CompletionRequestedEvent{},
				states.AllToolsProcessedEvent{},
				states.ToolsCompletedEvent{},
			} {
				assert.NoError(t, s.Handle(evt), "event %T", evt)
			}

			assertTransitions(t, f.sm)
			assertEvents(t, f.events)
		})
	}
}
