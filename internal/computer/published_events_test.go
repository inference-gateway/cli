package computer

import (
	"context"
	"strings"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

func TestPublishedEventOnTheAGUIStream(t *testing.T) {
	tests := []struct {
		name  string
		event agentdomain.ChatEvent
		want  string
	}{
		{"pause", agentdomain.ComputerUsePausedEvent{RequestID: "s1"}, `"name":"computer_use_paused","value":{"request_id":"s1"}`},
		{"resume", agentdomain.ComputerUseResumedEvent{RequestID: "s1"}, `"name":"computer_use_resumed","value":{"request_id":"s1"}`},
		{"recording started", agentdomain.ScreenRecordingStatusEvent{Active: true}, `"name":"screen_recording","value":{"active":true}`},
		{"recording stopped", agentdomain.ScreenRecordingStatusEvent{Active: false}, `"name":"screen_recording","value":{"active":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			custom, ok := PublishedEvent(tt.event)
			if !ok {
				t.Fatalf("PublishedEvent(%T) claimed nothing", tt.event)
			}
			var out strings.Builder
			agui.NewRun(&out).Publish(custom)
			if got := out.String(); !strings.Contains(got, `"type":"CUSTOM"`) || !strings.Contains(got, tt.want) {
				t.Errorf("missing %s in output:\n%s", tt.want, got)
			}
		})
	}
}

// TestResumeCarriesThePausedRunOn pauses a run, which cancels its turn, and
// resumes it: the run ends as a success instead of a cancellation.
func TestResumeCarriesThePausedRunOn(t *testing.T) {
	var out strings.Builder
	run := agui.NewRun(&out)
	run.Start("s1", "run-1")
	paused, _ := PublishedEvent(agentdomain.ComputerUsePausedEvent{RequestID: "s1"})
	run.Publish(paused)
	run.Fail(context.Canceled)
	resumed, _ := PublishedEvent(agentdomain.ComputerUseResumedEvent{RequestID: "s1"})
	run.Publish(resumed)

	if err := run.Finish(nil); err != nil {
		t.Fatalf("Finish() err = %v, want nil after the resume", err)
	}
	if got := out.String(); !strings.Contains(got, `"outcome":{"type":"success"}`) {
		t.Errorf("the resumed run did not end as a success:\n%s", got)
	}
}

func TestPublishedEventLeavesOtherEventsAlone(t *testing.T) {
	if _, ok := PublishedEvent(agentdomain.ChatCompleteEvent{}); ok {
		t.Fatal("PublishedEvent claimed an event the computer context does not own")
	}
}
