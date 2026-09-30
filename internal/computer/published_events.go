package computer

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// PublishedEvent maps the computer context's chat events into the run state they
// publish, so a client can show a recording indicator. A paused or resumed
// computer use ends and starts runs instead, which clients see through the run
// frames, so it publishes nothing.
func PublishedEvent(event agentdomain.ChatEvent) (agui.Published, bool) {
	switch ev := event.(type) {
	case agentdomain.ScreenRecordingStatusEvent:
		return agui.Published{StateKey: agui.StateScreenRecording, StateValue: map[string]any{"active": ev.Active}}, true
	}
	return agui.Published{}, false
}
