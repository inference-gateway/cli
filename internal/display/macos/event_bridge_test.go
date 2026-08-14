package macos

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// A control event (e.g. a tool approval) must reach the subscriber even when the
// buffer is saturated with streaming chunks; otherwise the panel never shows the
// approval card. Publish is non-blocking, so this is fully synchronous.
func TestPublish_DeliversControlEventWhenBufferFull(t *testing.T) {
	eb := NewEventBridge()
	ch := eb.Subscribe()

	for i := 0; i < cap(ch)*2; i++ {
		eb.Publish(domain.ChatChunkEvent{Content: "x"})
	}

	eb.Publish(domain.ToolApprovalRequestedEvent{RequestID: "r1"})

	found := false
	for len(ch) > 0 {
		if a, ok := (<-ch).(domain.ToolApprovalRequestedEvent); ok && a.RequestID == "r1" {
			found = true
		}
	}
	if !found {
		t.Fatal("approval event was dropped: control events must not be lost on a full buffer")
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
			eb.Publish(domain.ChatChunkEvent{Content: "x"})
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
