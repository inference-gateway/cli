package states_test

import (
	"context"
	"errors"
	states "github.com/inference-gateway/cli/internal/agent/states"
	statesmocks "github.com/inference-gateway/cli/tests/mocks/states"
	"sync"
	"testing"

	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
)

var errBoom = errors.New("boom")

// completeCall records one PublishChatComplete invocation.
type completeCall struct {
	reasoning string
	toolCalls []sdk.ChatCompletionMessageToolCall
}

// stateFixture wires a states.StateContext against the counterfeiter fakes
// (FakeAgentStateMachine, FakeMessageQueue) with recording stubs for every
// callback the state executors touch. Defaults: empty queue, live session
// context, no tool calls, no approval required, DispatchHooks nil (the
// executors' nil-guard path) unless recordHooks is called.
type stateFixture struct {
	ctx    *states.StateContext
	sm     *statesmocks.FakeAgentStateMachine
	queue  *convmocks.FakeMessageQueue
	events chan states.AgentEvent

	drainReturns  int
	drainCalls    int
	completeCalls []completeCall
	cancelCalls   int
	added         []convdomain.ConversationEntry
}

func newStateFixture() *stateFixture {
	f := &stateFixture{
		sm:     &statesmocks.FakeAgentStateMachine{},
		queue:  &convmocks.FakeMessageQueue{},
		events: make(chan states.AgentEvent, 16),
	}
	f.queue.IsEmptyReturns(true)

	conv := []sdk.Message{}
	msg := sdk.Message{}
	toolCalls := []*sdk.ChatCompletionMessageToolCall{}
	reasoning := ""
	idx := 0
	results := []convdomain.ConversationEntry{}

	f.ctx = &states.StateContext{
		StateMachine: f.sm,
		AgentCtx: &states.AgentContext{
			Ctx:          context.Background(),
			MessageQueue: f.queue,
			Conversation: &conv,
		},
		Events:                f.events,
		WaitGroup:             &sync.WaitGroup{},
		Mutex:                 &sync.Mutex{},
		CurrentMessage:        &msg,
		CurrentToolCalls:      &toolCalls,
		CurrentReasoning:      &reasoning,
		CurrentToolIndex:      &idx,
		ToolResults:           &results,
		Request:               &agentdomain.AgentRequest{RequestID: "req-1"},
		GetMetrics:            func(string) *agentdomain.ChatMetrics { return nil },
		ShouldRequireApproval: func(*sdk.ChatCompletionMessageToolCall, bool) bool { return false },
		AddMessage: func(e convdomain.ConversationEntry) error {
			f.added = append(f.added, e)
			return nil
		},
		BatchDrainQueue: func() int {
			f.drainCalls++
			return f.drainReturns
		},
		ExecuteToolInternal: func(tc sdk.ChatCompletionMessageToolCall, _ bool) convdomain.ConversationEntry {
			return toolEntry(tc)
		},
		PublishChatEvent: func(agentdomain.ChatEvent) {},
		PublishChatComplete: func(reasoning string, tcs []sdk.ChatCompletionMessageToolCall, _ *agentdomain.ChatMetrics) {
			f.completeCalls = append(f.completeCalls, completeCall{reasoning: reasoning, toolCalls: tcs})
		},
		PublishChatCancelled: func(*agentdomain.ChatMetrics) { f.cancelCalls++ },
	}
	return f
}

// recordHooks installs a DispatchHooks recorder and returns the dispatched
// hook points (nil until the first dispatch, so it compares equal to an
// absent expectation).
func (f *stateFixture) recordHooks() *[]agentdomain.HookPoint {
	var hooks []agentdomain.HookPoint
	f.ctx.DispatchHooks = func(h agentdomain.HookPoint) { hooks = append(hooks, h) }
	return &hooks
}

// cancelSession replaces the session context with an already-cancelled one.
func (f *stateFixture) cancelSession() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.ctx.AgentCtx.Ctx = ctx
}

// assertTransitions asserts the exact sequence of Transition targets requested
// on the fake state machine.
func assertTransitions(t *testing.T, sm *statesmocks.FakeAgentStateMachine, want ...states.AgentExecutionState) {
	t.Helper()
	require.Equal(t, len(want), sm.TransitionCallCount(), "unexpected number of transitions")
	for i, target := range want {
		_, got := sm.TransitionArgsForCall(i)
		assert.Equal(t, target, got, "transition %d target", i)
	}
}

// assertEvents drains the buffered events channel and asserts the emitted
// events match want by type, in order.
func assertEvents(t *testing.T, events chan states.AgentEvent, want ...states.AgentEvent) {
	t.Helper()
	var got []states.AgentEvent
drain:
	for {
		select {
		case e := <-events:
			got = append(got, e)
		default:
			break drain
		}
	}
	require.Len(t, got, len(want), "unexpected emitted events: %v", got)
	for i, w := range want {
		assert.IsType(t, w, got[i], "event %d", i)
	}
}
