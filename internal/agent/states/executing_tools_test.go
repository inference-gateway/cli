package states_test

import (
	"testing"

	states "github.com/inference-gateway/cli/internal/agent/states"

	assert "github.com/stretchr/testify/assert"
)

// TestExecutingToolsState_Handle covers the two states.ToolsCompletedEvent routes
// (continue to PostToolExecution, or Stop=true → the Stopped terminal),
// transition-failure propagation on both, and stray-event tolerance.
func TestExecutingToolsState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		event           states.AgentEvent
		transitionErr   error
		wantErr         bool
		wantTransitions []states.AgentExecutionState
		wantEvents      []states.AgentEvent
	}{
		{
			name:            "completed tools continue to post tool execution",
			event:           states.ToolsCompletedEvent{},
			wantTransitions: []states.AgentExecutionState{states.StatePostToolExecution},
			wantEvents:      []states.AgentEvent{states.MessageReceivedEvent{}},
		},
		{
			name:            "stop signal terminates the loop",
			event:           states.ToolsCompletedEvent{Stop: true},
			wantTransitions: []states.AgentExecutionState{states.StateStopped},
		},
		{
			name:            "transition failure is returned",
			event:           states.ToolsCompletedEvent{},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []states.AgentExecutionState{states.StatePostToolExecution},
		},
		{
			name:            "stop transition failure is returned",
			event:           states.ToolsCompletedEvent{Stop: true},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []states.AgentExecutionState{states.StateStopped},
		},
		{
			name:  "stray event is a no-op",
			event: states.MessageReceivedEvent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			f.sm.TransitionReturns(tt.transitionErr)
			s := states.NewExecutingToolsState(f.ctx)
			assert.Equal(t, states.StateExecutingTools, s.Name())

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
