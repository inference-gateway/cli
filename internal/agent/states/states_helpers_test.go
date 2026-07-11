package states

import (
	"context"
	"errors"
	"sync"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

var errBoom = errors.New("boom")

// completeCall records one PublishChatComplete invocation.
type completeCall struct {
	reasoning string
	toolCalls []sdk.ChatCompletionMessageToolCall
}

// stateFixture wires a StateContext against the counterfeiter fakes
// (FakeAgentStateMachine, FakeMessageQueue) with recording stubs for every
// callback the state executors touch. Defaults: empty queue, live session
// context, no tool calls, no approval required, DispatchHooks nil (the
// executors' nil-guard path) unless recordHooks is called.
type stateFixture struct {
	ctx    *domain.StateContext
	sm     *domainmocks.FakeAgentStateMachine
	queue  *domainmocks.FakeMessageQueue
	events chan domain.AgentEvent

	drainReturns  int
	drainCalls    int
	completeCalls []completeCall
	cancelCalls   int
	added         []domain.ConversationEntry
}

func newStateFixture() *stateFixture {
	f := &stateFixture{
		sm:     &domainmocks.FakeAgentStateMachine{},
		queue:  &domainmocks.FakeMessageQueue{},
		events: make(chan domain.AgentEvent, 16),
	}
	f.queue.IsEmptyReturns(true)

	conv := []sdk.Message{}
	msg := sdk.Message{}
	toolCalls := []*sdk.ChatCompletionMessageToolCall{}
	reasoning := ""
	idx := 0
	results := []domain.ConversationEntry{}

	f.ctx = &domain.StateContext{
		StateMachine: f.sm,
		AgentCtx: &domain.AgentContext{
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
		Request:               &domain.AgentRequest{RequestID: "req-1"},
		GetMetrics:            func(string) *domain.ChatMetrics { return nil },
		ShouldRequireApproval: func(*sdk.ChatCompletionMessageToolCall, bool) bool { return false },
		AddMessage: func(e domain.ConversationEntry) error {
			f.added = append(f.added, e)
			return nil
		},
		BatchDrainQueue: func() int {
			f.drainCalls++
			return f.drainReturns
		},
		ExecuteToolInternal: func(tc sdk.ChatCompletionMessageToolCall, _ bool) domain.ConversationEntry {
			return toolEntry(tc)
		},
		PublishChatEvent: func(domain.ChatEvent) {},
		PublishChatComplete: func(reasoning string, tcs []sdk.ChatCompletionMessageToolCall, _ *domain.ChatMetrics) {
			f.completeCalls = append(f.completeCalls, completeCall{reasoning: reasoning, toolCalls: tcs})
		},
		PublishChatCancelled: func(*domain.ChatMetrics) { f.cancelCalls++ },
	}
	return f
}

// recordHooks installs a DispatchHooks recorder and returns the dispatched
// hook points (nil until the first dispatch, so it compares equal to an
// absent expectation).
func (f *stateFixture) recordHooks() *[]domain.HookPoint {
	var hooks []domain.HookPoint
	f.ctx.DispatchHooks = func(h domain.HookPoint) { hooks = append(hooks, h) }
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
func assertTransitions(t *testing.T, sm *domainmocks.FakeAgentStateMachine, want ...domain.AgentExecutionState) {
	t.Helper()
	require.Equal(t, len(want), sm.TransitionCallCount(), "unexpected number of transitions")
	for i, target := range want {
		_, got := sm.TransitionArgsForCall(i)
		assert.Equal(t, target, got, "transition %d target", i)
	}
}

// assertEvents drains the buffered events channel and asserts the emitted
// events match want by type, in order.
func assertEvents(t *testing.T, events chan domain.AgentEvent, want ...domain.AgentEvent) {
	t.Helper()
	var got []domain.AgentEvent
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
