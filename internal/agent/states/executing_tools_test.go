package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
)

// TestExecutingToolsState_Handle covers the two ToolsCompletedEvent routes
// (continue to PostToolExecution, or Stop=true → the Stopped terminal),
// transition-failure propagation on both, and stray-event tolerance.
func TestExecutingToolsState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		event           AgentEvent
		transitionErr   error
		wantErr         bool
		wantTransitions []AgentExecutionState
		wantEvents      []AgentEvent
	}{
		{
			name:            "completed tools continue to post tool execution",
			event:           ToolsCompletedEvent{},
			wantTransitions: []AgentExecutionState{StatePostToolExecution},
			wantEvents:      []AgentEvent{MessageReceivedEvent{}},
		},
		{
			name:            "stop signal terminates the loop",
			event:           ToolsCompletedEvent{Stop: true},
			wantTransitions: []AgentExecutionState{StateStopped},
		},
		{
			name:            "transition failure is returned",
			event:           ToolsCompletedEvent{},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []AgentExecutionState{StatePostToolExecution},
		},
		{
			name:            "stop transition failure is returned",
			event:           ToolsCompletedEvent{Stop: true},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []AgentExecutionState{StateStopped},
		},
		{
			name:  "stray event is a no-op",
			event: MessageReceivedEvent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			f.sm.TransitionReturns(tt.transitionErr)
			s := NewExecutingToolsState(f.ctx)
			assert.Equal(t, StateExecutingTools, s.Name())

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
