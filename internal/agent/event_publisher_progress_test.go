package agent

import (
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TestPublishToolProgressDropsInsteadOfBlocking verifies progress updates do
// not stall their tool when the consumer falls behind.
func TestPublishToolProgressDropsInsteadOfBlocking(t *testing.T) {
	events := make(chan agentdomain.ChatEvent)
	p := newEventPublisher("req-1", events)

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.publishToolProgress("call-1", "TextToSpeech", "Downloading tts model: 12 MB of 900 MB (1%)")
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishToolProgress blocked on a full channel")
	}
}

func TestPublishToolProgressDeliversWhenConsumerKeepsUp(t *testing.T) {
	events := make(chan agentdomain.ChatEvent, 1)
	p := newEventPublisher("req-1", events)

	p.publishToolProgress("call-1", "TextToSpeech", "Downloading tts model: 12 MB")

	select {
	case ev := <-events:
		progress, ok := ev.(agentdomain.ToolExecutionProgressEvent)
		if !ok {
			t.Fatalf("event type = %T, want ToolExecutionProgressEvent", ev)
		}
		if progress.Status != "running" {
			t.Errorf("Status = %q, want running", progress.Status)
		}
		if progress.ToolCallID != "call-1" || progress.ToolName != "TextToSpeech" {
			t.Errorf("event = %+v, want it to identify the call", progress)
		}
		if progress.Message != "Downloading tts model: 12 MB" {
			t.Errorf("Message = %q", progress.Message)
		}
	default:
		t.Fatal("expected the progress event to be delivered")
	}
}
