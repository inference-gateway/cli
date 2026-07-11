package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestPostToolExecutionState_Handle covers the routing after a completed tool
// batch: an empty queue either completes or continues to the next turn, a
// non-empty queue is drained (with drain hooks) before continuing, and a
// cancelled session drains then completes without another LLM turn (the
// issue #532 contract). The post_tool hook fires on every path.
func TestPostToolExecutionState_Handle(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(f *stateFixture)
		transitionErr   error
		wantErr         bool
		wantTransitions []domain.AgentExecutionState
		wantEvents      []domain.AgentEvent
		wantHooks       []domain.HookPoint
		wantDrainCalls  int
	}{
		{
			name:            "queue empty and can complete",
			setup:           func(f *stateFixture) { f.sm.CanTransitionReturns(true) },
			wantTransitions: []domain.AgentExecutionState{domain.StateCompleting},
			wantEvents:      []domain.AgentEvent{domain.CompletionRequestedEvent{}},
			wantHooks:       []domain.HookPoint{domain.HookPostTool},
		},
		{
			name:            "queue empty and cannot complete continues to next turn",
			wantTransitions: []domain.AgentExecutionState{domain.StateCheckingQueue},
			wantEvents:      []domain.AgentEvent{domain.MessageReceivedEvent{}},
			wantHooks:       []domain.HookPoint{domain.HookPostTool},
		},
		{
			name: "queued messages are drained before the next turn",
			setup: func(f *stateFixture) {
				f.queue.IsEmptyReturns(false)
				f.drainReturns = 1
			},
			wantTransitions: []domain.AgentExecutionState{domain.StateCheckingQueue},
			wantEvents:      []domain.AgentEvent{domain.MessageReceivedEvent{}},
			wantHooks:       []domain.HookPoint{domain.HookPostTool, domain.HookPreQueueDrain, domain.HookPostQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name: "cancelled session drains then completes without another turn",
			setup: func(f *stateFixture) {
				f.queue.IsEmptyReturns(false)
				f.drainReturns = 1
				f.cancelSession()
			},
			wantTransitions: []domain.AgentExecutionState{domain.StateCompleting},
			wantEvents:      []domain.AgentEvent{domain.CompletionRequestedEvent{}},
			wantHooks:       []domain.HookPoint{domain.HookPostTool, domain.HookPreQueueDrain, domain.HookPostQueueDrain},
			wantDrainCalls:  1,
		},
		{
			name:            "transition failure is returned",
			transitionErr:   errBoom,
			wantErr:         true,
			wantTransitions: []domain.AgentExecutionState{domain.StateCheckingQueue},
			wantHooks:       []domain.HookPoint{domain.HookPostTool},
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
			s := NewPostToolExecutionState(f.ctx)
			assert.Equal(t, domain.StatePostToolExecution, s.Name())

			err := s.Handle(domain.MessageReceivedEvent{})

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
