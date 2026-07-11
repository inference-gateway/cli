package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestPostStreamState_Handle covers the routing after a completed stream:
// queued messages win over everything, tool calls route to EvaluatingTools,
// no tools after at least one turn completes (publishing the final chat event
// and clearing HasToolResults), and turn zero continues the loop.
func TestPostStreamState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(f *stateFixture)
		transitionErr   error
		wantErr         bool
		wantTransitions []domain.AgentExecutionState
		wantEvents      []domain.AgentEvent
		check           func(t *testing.T, f *stateFixture)
	}{
		{
			name:            "queued messages return to checking queue",
			setup:           func(f *stateFixture) { f.queue.IsEmptyReturns(false) },
			wantTransitions: []domain.AgentExecutionState{domain.StateCheckingQueue},
			wantEvents:      []domain.AgentEvent{domain.MessageReceivedEvent{}},
		},
		{
			name:            "tool calls route to evaluating tools",
			setup:           func(f *stateFixture) { *f.ctx.CurrentToolCalls = makeTools(1) },
			wantTransitions: []domain.AgentExecutionState{domain.StateEvaluatingTools},
			wantEvents:      []domain.AgentEvent{domain.MessageReceivedEvent{}},
		},
		{
			name: "no tools after a turn completes",
			setup: func(f *stateFixture) {
				f.ctx.AgentCtx.Turns = 1
				f.ctx.AgentCtx.HasToolResults = true
			},
			wantTransitions: []domain.AgentExecutionState{domain.StateCompleting},
			wantEvents:      []domain.AgentEvent{domain.CompletionRequestedEvent{}},
			check: func(t *testing.T, f *stateFixture) {
				assert.False(t, f.ctx.AgentCtx.HasToolResults, "HasToolResults must be cleared")
				require.Len(t, f.completeCalls, 1, "final chat completion must be published")
				assert.Empty(t, f.completeCalls[0].toolCalls)
			},
		},
		{
			name:            "no tools on turn zero continues the loop",
			wantTransitions: []domain.AgentExecutionState{domain.StateCheckingQueue},
			wantEvents:      []domain.AgentEvent{domain.MessageReceivedEvent{}},
		},
		{
			name:            "transition failure is returned",
			setup:           func(f *stateFixture) { f.ctx.AgentCtx.Turns = 1 },
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []domain.AgentExecutionState{domain.StateCompleting},
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
			s := NewPostStreamState(f.ctx)
			assert.Equal(t, domain.StatePostStream, s.Name())

			err := s.Handle(domain.MessageReceivedEvent{})

			if tt.wantErr {
				assert.ErrorIs(t, err, errBoom)
			} else {
				assert.NoError(t, err)
			}
			assertTransitions(t, f.sm, tt.wantTransitions...)
			assertEvents(t, f.events, tt.wantEvents...)
			assert.Equal(t, []domain.HookPoint{domain.HookPostStream}, *hooks, "post_stream hook dispatched on every path")
			if tt.check != nil {
				tt.check(t, f)
			}
		})
	}
}
