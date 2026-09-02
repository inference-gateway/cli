package handlers

import (
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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
		stale := make(chan agentdomain.ChatEvent)
		if cmd := h.Handle(tui.ChatChannelEvent{Event: unknownEvent{"stale"}, Source: stale}); cmd != nil {
			t.Fatal("event from a previous turn's channel must not add a listener")
		}
	})
}
