package computer

import (
	"strings"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

func TestPublishedEventPatchesTheRunState(t *testing.T) {
	tests := []struct {
		name  string
		event agentdomain.ChatEvent
		want  string
	}{
		{"recording started", agentdomain.ScreenRecordingStatusEvent{Active: true}, `{"active":true}`},
		{"recording stopped", agentdomain.ScreenRecordingStatusEvent{Active: false}, `{"active":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			published, ok := PublishedEvent(tt.event)
			if !ok {
				t.Fatalf("PublishedEvent(%T) claimed nothing", tt.event)
			}
			var out strings.Builder
			run := agui.NewRun(&out)
			run.Start("s1", "run-1")
			run.PatchState(published.StateKey, published.StateValue)
			if !strings.Contains(out.String(), `"`+published.StateKey+`"`) || !strings.Contains(out.String(), tt.want) {
				t.Errorf("missing the state patch in output:\n%s", out.String())
			}
		})
	}
}

// TestComputerUsePausePublishesNothing: pausing and resuming computer use stop
// and start runs, which clients see through the run frames, so the events
// publish nothing.
func TestComputerUsePausePublishesNothing(t *testing.T) {
	for _, event := range []agentdomain.ChatEvent{
		agentdomain.ComputerUsePausedEvent{RequestID: "s1"},
		agentdomain.ComputerUseResumedEvent{RequestID: "s1"},
	} {
		if published, ok := PublishedEvent(event); ok {
			t.Fatalf("PublishedEvent(%T) published %+v, want nothing", event, published)
		}
	}
}

func TestPublishedEventLeavesOtherEventsAlone(t *testing.T) {
	if _, ok := PublishedEvent(agentdomain.ChatCompleteEvent{}); ok {
		t.Fatal("PublishedEvent claimed an event the computer context does not own")
	}
}
