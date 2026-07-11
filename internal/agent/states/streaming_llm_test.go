package states

import (
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestStreamingLLMState_StartStreamingSpawnsGoroutine verifies that a
// StartStreamingEvent launches the StartStreaming callback on a background
// goroutine tracked by the shared WaitGroup, without transitioning or
// emitting anything itself.
func TestStreamingLLMState_StartStreamingSpawnsGoroutine(t *testing.T) {
	f := newStateFixture()
	started := make(chan struct{})
	f.ctx.StartStreaming = func() { close(started) }
	s := NewStreamingLLMState(f.ctx)
	assert.Equal(t, domain.StateStreamingLLM, s.Name())

	require.NoError(t, s.Handle(domain.StartStreamingEvent{}))

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
	evt := domain.StreamCompletedEvent{
		Message:   sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("hello")},
		ToolCalls: tools,
		Reasoning: "thinking",
	}
	s := NewStreamingLLMState(f.ctx)

	require.NoError(t, s.Handle(evt))

	assert.Equal(t, evt.Message, *f.ctx.CurrentMessage)
	assert.Equal(t, tools, *f.ctx.CurrentToolCalls)
	assert.Equal(t, "thinking", *f.ctx.CurrentReasoning)
	assert.Equal(t, tools, f.ctx.AgentCtx.ToolCalls)
	assertTransitions(t, f.sm, domain.StatePostStream)
	assertEvents(t, f.events, domain.MessageReceivedEvent{})
}

// TestStreamingLLMState_TransitionFailureIsReturned verifies a failed
// transition to PostStream surfaces the error and emits nothing.
func TestStreamingLLMState_TransitionFailureIsReturned(t *testing.T) {
	f := newStateFixture()
	f.sm.TransitionReturns(errBoom)
	s := NewStreamingLLMState(f.ctx)

	err := s.Handle(domain.StreamCompletedEvent{})

	assert.ErrorIs(t, err, errBoom)
	assertEvents(t, f.events)
}
