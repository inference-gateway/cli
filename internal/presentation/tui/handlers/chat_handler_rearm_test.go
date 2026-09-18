package handlers

import (
	"fmt"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

// unknownEvent is a ChatEvent with no dispatch case, standing in for any event
// type added later without touching the handler.
type unknownEvent struct{ id string }

func (e unknownEvent) GetRequestID() string    { return e.id }
func (e unknownEvent) GetTimestamp() time.Time { return time.Time{} }

// TestHandle_RearmsChatListener pins the one re-arm site: an event read off the
// chat channel keeps the chain alive even when nothing handles it, while events
// from anywhere else, or from a channel that is no longer the session's, never
// add a reader.
func TestHandle_RearmsChatListener(t *testing.T) {
	sm := statemanager.NewStateManager(false)
	events := make(chan agentdomain.ChatEvent, 1)
	if err := sm.StartChatSession("req-1", "test/model", events); err != nil {
		t.Fatalf("StartChatSession: %v", err)
	}
	h := &ChatHandler{stateManager: sm}

	t.Run("unknown event from the chat channel re-arms", func(t *testing.T) {
		cmd := h.Handle(tui.ChatChannelEvent{Event: unknownEvent{"first"}, Source: events})
		if cmd == nil {
			t.Fatal("no listener re-armed for an event read off the chat channel")
		}
		next := unknownEvent{"next"}
		events <- next
		got, ok := cmd().(tui.ChatChannelEvent)
		if !ok || got.Event != next || got.Source != (<-chan agentdomain.ChatEvent)(events) {
			t.Fatalf("re-armed listener did not read the next chat event, got %#v", got)
		}
	})

	t.Run("same event off-channel does not arm", func(t *testing.T) {
		if cmd := h.Handle(unknownEvent{"off"}); cmd != nil {
			t.Fatal("off-channel event must not add a chat listener")
		}
	})

	t.Run("stale source does not arm", func(t *testing.T) {
		runner := &tuimocks.FakeChatCompletionRunner{}
		h.completionRunner = runner
		stale := make(chan agentdomain.ChatEvent)
		if cmd := h.Handle(tui.ChatChannelEvent{Event: agentdomain.ChatChunkEvent{Content: "stale"}, Source: stale}); cmd != nil {
			t.Fatal("event from a previous turn's channel must not add a listener")
		}
		if runner.HandleChatChunkCallCount() != 0 {
			t.Fatal("stale event must not be dispatched")
		}
	})
}

func TestHandle_ChatStreamOpened(t *testing.T) {
	for _, bridged := range []bool{false, true} {
		t.Run(fmt.Sprintf("bridged=%v", bridged), func(t *testing.T) {
			sm := statemanager.NewStateManager(false)
			sm.SetChatPending()
			if bridged {
				sm.SetEventBridge(conversation.NewEventBridge())
			}
			events := make(chan agentdomain.ChatEvent, 1)
			first := agentdomain.ChatStartEvent{RequestID: "req"}
			events <- first
			close(events)
			h := &ChatHandler{stateManager: sm}
			cmd := h.Handle(tui.ChatStreamOpenedEvent{
				Session: sm.GetChatSession(), RequestID: "req", Model: "test/model", Events: events,
			})
			if cmd == nil {
				t.Fatal("stream initialization must arm its listener")
			}
			event, ok := cmd().(tui.ChatChannelEvent)
			if !ok || event.Event != first || event.Source != sm.GetChatSession().EventChannel {
				t.Fatalf("initial listener must use the session's channel, got %+v", event)
			}
		})
	}

	for _, replaced := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled, replaced=%v", replaced), func(t *testing.T) {
			sm := statemanager.NewStateManager(false)
			sm.SetChatPending()
			pending := sm.GetChatSession()
			sm.EndChatSession()
			if replaced {
				sm.SetChatPending()
			}
			current := sm.GetChatSession()
			agent := &agentdomainmocks.FakeAgentService{}
			events := make(chan agentdomain.ChatEvent, 1)
			events <- agentdomain.ChatCompleteEvent{RequestID: "old"}
			close(events)
			h := &ChatHandler{stateManager: sm, agentService: agent}
			cmd := h.Handle(tui.ChatStreamOpenedEvent{Session: pending, RequestID: "old", Events: events})
			if sm.GetChatSession() != current {
				t.Fatal("late startup replaced the current session")
			}
			if cmd == nil || cmd() != nil {
				t.Fatal("late startup must cancel and drain without emitting UI events")
			}
			if agent.CancelRequestCallCount() != 1 || agent.CancelRequestArgsForCall(0) != "old" || len(events) != 0 {
				t.Fatal("abandoned request was not cancelled and drained")
			}
		})
	}
}

func TestHandle_ChatCompletionRequested(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%v", cancelled), func(t *testing.T) {
			sm := statemanager.NewStateManager(false)
			sm.SetChatPending()
			request := tui.ChatCompletionRequestedEvent{Session: sm.GetChatSession()}
			if cancelled {
				sm.EndChatSession()
			}
			runner := &tuimocks.FakeChatCompletionRunner{}
			h := &ChatHandler{stateManager: sm, completionRunner: runner}
			h.Handle(request)
			if started := runner.StartCallCount() != 0; started == cancelled {
				t.Fatalf("started=%v, cancelled=%v", started, cancelled)
			}
		})
	}
}
