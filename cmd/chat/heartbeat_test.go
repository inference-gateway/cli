package chat

import (
	"context"
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

type chanNotifier chan any

func (c chanNotifier) Notify(event any) { c <- event }

// TestRunUIHeartbeat checks the heartbeat pushes HeartbeatEvents at the interval
// and stops cleanly when the context is cancelled.
func TestRunUIHeartbeat(t *testing.T) {
	events := make(chanNotifier, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runUIHeartbeat(ctx, events, 5*time.Millisecond)
	}()

	select {
	case ev := <-events:
		if _, ok := ev.(agentdomain.HeartbeatEvent); !ok {
			t.Fatalf("got %T, want HeartbeatEvent", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no heartbeat within 1s")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat goroutine did not stop after cancel")
	}
}
