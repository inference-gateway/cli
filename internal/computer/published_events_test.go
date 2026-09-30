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
		{
			"recording started with its frame",
			agentdomain.ScreenRecordingStatusEvent{
				Active: true, Path: "/tmp/infer/a.mp4",
				RegionX: 128, RegionY: 96, RegionWidth: 512, RegionHeight: 384,
				FrameWidth: 1024, FrameHeight: 768,
			},
			`{"active":true,"frameHeight":768,"frameWidth":1024,"path":"/tmp/infer/a.mp4","region":{"height":384,"width":512,"x":128,"y":96}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			published, ok := PublishedEvent(tt.event)
			if !ok {
				t.Fatalf("PublishedEvent(%T) claimed nothing", tt.event)
			}
			if published.StateKey != agui.StateScreenRecording {
				t.Fatalf("StateKey = %q, want %q", published.StateKey, agui.StateScreenRecording)
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

func TestPublishedEventMapsComputerUseAction(t *testing.T) {
	tests := []struct {
		name    string
		event   agentdomain.ComputerUseActionEvent
		want    map[string]any
		wantAbs []string
	}{
		{
			name:  "pointer action carries its screen coordinates",
			event: agentdomain.ComputerUseActionEvent{ToolCallID: "tc1", Action: "click", X: 640, Y: 512, ScreenWidth: 1920, ScreenHeight: 1080},
			want: map[string]any{
				"toolCallId": "tc1", "action": "click", "x": 640, "y": 512, "screenWidth": 1920, "screenHeight": 1080,
			},
		},
		{
			name:  "keyboard action carries no coordinates",
			event: agentdomain.ComputerUseActionEvent{ToolCallID: "tc2", Action: "type", ScreenWidth: 1920, ScreenHeight: 1080},
			want: map[string]any{
				"toolCallId": "tc2", "action": "type", "screenWidth": 1920, "screenHeight": 1080,
			},
			wantAbs: []string{"x", "y"},
		},
		{
			name:  "keyboard combo carries no coordinates",
			event: agentdomain.ComputerUseActionEvent{ToolCallID: "tc3", Action: "key", ScreenWidth: 1920, ScreenHeight: 1080},
			want: map[string]any{
				"toolCallId": "tc3", "action": "key", "screenWidth": 1920, "screenHeight": 1080,
			},
			wantAbs: []string{"x", "y"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			published, ok := PublishedEvent(tt.event)
			if !ok {
				t.Fatalf("PublishedEvent(%T) claimed nothing", tt.event)
			}
			if published.ActivityType != "computer_use" || published.MessageID != "computer_use:"+tt.event.ToolCallID {
				t.Fatalf("activity identity = %q/%q, want computer_use/computer_use:%s", published.ActivityType, published.MessageID, tt.event.ToolCallID)
			}
			content, ok := published.Content.(map[string]any)
			if !ok || len(content) != len(tt.want) {
				t.Fatalf("content = %+v, want exactly %+v", published.Content, tt.want)
			}
			for key, want := range tt.want {
				if got := content[key]; got != want {
					t.Errorf("content[%q] = %v, want %v", key, got, want)
				}
			}
			for _, absent := range tt.wantAbs {
				if _, ok := content[absent]; ok {
					t.Errorf("content carries %q, want it absent", absent)
				}
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
