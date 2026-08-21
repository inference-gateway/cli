package states_test

import (
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	states "github.com/inference-gateway/cli/internal/agent/states"
)

// TestStreamingLLMState_StartStreamingSpawnsGoroutine verifies that a
// states.StartStreamingEvent launches the StartStreaming callback on a background
// goroutine tracked by the shared WaitGroup, without transitioning or
// emitting anything itself.
func TestStreamingLLMState_StartStreamingSpawnsGoroutine(t *testing.T) {
	f := newStateFixture()
	started := make(chan struct{})
	f.ctx.StartStreaming = func() { close(started) }
	s := states.NewStreamingLLMState(f.ctx)
	assert.Equal(t, states.StateStreamingLLM, s.Name())

	require.NoError(t, s.Handle(states.StartStreamingEvent{}))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("StartStreaming was not invoked")
	}
	f.ctx.WaitGroup.Wait()

	assertTransitions(t, f.sm)
	assertEvents(t, f.events)
}

// TestStreamingLLMState_StreamCompletedStoresDataAndAdvances verifies the
// completed stream's message, tool calls and reasoning are stored into the
// shared state (and AgentCtx.ToolCalls for the transition guards) before
// advancing to PostStream.
func TestStreamingLLMState_StreamCompletedStoresDataAndAdvances(t *testing.T) {
	f := newStateFixture()
	tools := makeTools(2)
	evt := states.StreamCompletedEvent{
		Message:   sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("hello")},
		ToolCalls: tools,
		Reasoning: "thinking",
	}
	s := states.NewStreamingLLMState(f.ctx)

	require.NoError(t, s.Handle(evt))

	assert.Equal(t, evt.Message, *f.ctx.CurrentMessage)
	assert.Equal(t, tools, *f.ctx.CurrentToolCalls)
	assert.Equal(t, "thinking", *f.ctx.CurrentReasoning)
	assert.Equal(t, tools, f.ctx.AgentCtx.ToolCalls)
	assertTransitions(t, f.sm, states.StatePostStream)
	assertEvents(t, f.events, states.MessageReceivedEvent{})
}

// TestStreamingLLMState_TransitionFailureIsReturned verifies a failed
// transition to PostStream surfaces the error and emits nothing.
func TestStreamingLLMState_TransitionFailureIsReturned(t *testing.T) {
	f := newStateFixture()
	f.sm.TransitionReturns(errBoom)
	s := states.NewStreamingLLMState(f.ctx)

	err := s.Handle(states.StreamCompletedEvent{})

	assert.ErrorIs(t, err, errBoom)
	assertEvents(t, f.events)
}
