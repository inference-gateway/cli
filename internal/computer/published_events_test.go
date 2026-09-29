package computer

import (
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

func render(t *testing.T, events ...agentdomain.ChatEvent) (string, error) {
	t.Helper()
	stream := make(chan agentdomain.ChatEvent, len(events))
	for _, event := range events {
		stream <- event
	}
	close(stream)
	var out strings.Builder
	err := agui.Render(stream, &out, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil, PublishedEvent)
	return out.String(), err
}

func TestPublishedEventOnTheAGUIStream(t *testing.T) {
	tests := []struct {
		name   string
		events []agentdomain.ChatEvent
		want   []string
	}{
		{
			name: "pause and resume",
			events: []agentdomain.ChatEvent{
				agentdomain.ComputerUsePausedEvent{RequestID: "s1"},
				agentdomain.ChatCompleteEvent{Cancelled: true},
				agentdomain.ComputerUseResumedEvent{RequestID: "s1"},
				agentdomain.ChatCompleteEvent{},
			},
			want: []string{
				`"name":"computer_use_paused","value":{"request_id":"s1"}`,
				`"name":"computer_use_resumed","value":{"request_id":"s1"}`,
				`"outcome":{"type":"success"}`,
			},
		},
		{
			name: "screen recording",
			events: []agentdomain.ChatEvent{
				agentdomain.ScreenRecordingStatusEvent{Active: true},
				agentdomain.ScreenRecordingStatusEvent{Active: false},
				agentdomain.ChatCompleteEvent{},
			},
			want: []string{
				`"name":"screen_recording","value":{"active":true}`,
				`"name":"screen_recording","value":{"active":false}`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := render(t, tt.events...)
			if err != nil {
				t.Fatalf("Render() err = %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %s in output:\n%s", want, got)
				}
			}
			if strings.Contains(got, `"RUN_ERROR"`) {
				t.Errorf("the run must not end in RUN_ERROR\n%s", got)
			}
		})
	}
}

func TestPublishedEventLeavesOtherEventsAlone(t *testing.T) {
	if _, ok := PublishedEvent(agentdomain.ChatCompleteEvent{}); ok {
		t.Fatal("PublishedEvent claimed an event the computer context does not own")
	}
}
