package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestExecutingToolsState_Handle covers the two ToolsCompletedEvent routes
// (continue to PostToolExecution, or Stop=true → the Stopped terminal),
// transition-failure propagation on both, and stray-event tolerance.
func TestExecutingToolsState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		event           domain.AgentEvent
		transitionErr   error
		wantErr         bool
		wantTransitions []domain.AgentExecutionState
		wantEvents      []domain.AgentEvent
	}{
		{
			name:            "completed tools continue to post tool execution",
			event:           domain.ToolsCompletedEvent{},
			wantTransitions: []domain.AgentExecutionState{domain.StatePostToolExecution},
			wantEvents:      []domain.AgentEvent{domain.MessageReceivedEvent{}},
		},
		{
			name:            "stop signal terminates the loop",
			event:           domain.ToolsCompletedEvent{Stop: true},
			wantTransitions: []domain.AgentExecutionState{domain.StateStopped},
		},
		{
			name:            "transition failure is returned",
			event:           domain.ToolsCompletedEvent{},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []domain.AgentExecutionState{domain.StatePostToolExecution},
		},
		{
			name:            "stop transition failure is returned",
			event:           domain.ToolsCompletedEvent{Stop: true},
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []domain.AgentExecutionState{domain.StateStopped},
		},
		{
			name:  "stray event is a no-op",
			event: domain.MessageReceivedEvent{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStateFixture()
			f.sm.TransitionReturns(tt.transitionErr)
			s := NewExecutingToolsState(f.ctx)
			assert.Equal(t, domain.StateExecutingTools, s.Name())

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
