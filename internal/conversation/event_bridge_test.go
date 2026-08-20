package conversation

import (
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// A control event (e.g. a tool approval) must reach the subscriber even when the
// buffer is saturated with streaming chunks; otherwise the panel never shows the
// approval card. Publish is non-blocking, so this is fully synchronous.
func TestPublish_DeliversControlEventWhenBufferFull(t *testing.T) {
	eb := NewEventBridge()
	ch := eb.Subscribe()

	for i := 0; i < cap(ch)*2; i++ {
		eb.Publish(agentdomain.ChatChunkEvent{Content: "x"})
	}

	eb.Publish(agentdomain.ToolApprovalRequestedEvent{RequestID: "r1"})

	found := false
	for len(ch) > 0 {
		if a, ok := (<-ch).(agentdomain.ToolApprovalRequestedEvent); ok && a.RequestID == "r1" {
			found = true
		}
	}
	if !found {
		t.Fatal("approval event was dropped: control events must not be lost on a full buffer")
	}
}

// SubscribeFuture must not replay the ring buffer: subscribers that backfill
// history another way (the extension bridge snapshot) would otherwise
// double-render the last turn (issue #1067). Subscribe, by contrast, replays.
func TestSubscribeFuture_SkipsRingBuffer(t *testing.T) {
	eb := NewEventBridge()

	eb.Publish(agentdomain.ChatChunkEvent{Content: "past-1"})
	eb.Publish(agentdomain.ChatChunkEvent{Content: "past-2"})

	if replay := eb.Subscribe(); len(replay) != 2 {
		t.Fatalf("Subscribe replayed %d events, want 2", len(replay))
	}

	future := eb.SubscribeFuture()
	if len(future) != 0 {
		t.Fatalf("SubscribeFuture replayed %d buffered events, want 0", len(future))
	}

	eb.Publish(agentdomain.ChatChunkEvent{Content: "new"})
	if got := len(future); got != 1 {
		t.Fatalf("SubscribeFuture buffered %d events after one publish, want 1", got)
	}
	if ev := (<-future).(agentdomain.ChatChunkEvent); ev.Content != "new" {
		t.Fatalf("got %q, want the post-subscribe event", ev.Content)
	}
}

// Streaming chunks stay lossy so a slow subscriber can't apply backpressure to
// the whole bus over cosmetic deltas.
func TestPublish_DropsChunksWhenBufferFull(t *testing.T) {
	eb := NewEventBridge()
	ch := eb.Subscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < cap(ch)*2; i++ {
			eb.Publish(agentdomain.ChatChunkEvent{Content: "x"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("chunk publish blocked: streaming deltas must be droppable")
	}
	if got := len(ch); got > cap(ch) {
		t.Fatalf("buffered %d chunks, want <= cap %d", got, cap(ch))
	}
}
