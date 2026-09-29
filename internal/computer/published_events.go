package computer

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// PublishedEvent maps the computer context's chat events to the AG-UI CUSTOM
// events it publishes, so a client can show a pause or a recording indicator.
func PublishedEvent(event agentdomain.ChatEvent) (agui.CustomEvent, bool) {
	switch ev := event.(type) {
	case agentdomain.ComputerUsePausedEvent:
		return agui.CustomEvent{Name: "computer_use_paused", Value: map[string]string{"request_id": ev.RequestID}}, true
	case agentdomain.ComputerUseResumedEvent:
		return agui.CustomEvent{Name: "computer_use_resumed", Value: map[string]string{"request_id": ev.RequestID}, Resumes: true}, true
	case agentdomain.ScreenRecordingStatusEvent:
		return agui.CustomEvent{Name: "screen_recording", Value: map[string]bool{"active": ev.Active}}, true
	}
	return agui.CustomEvent{}, false
}
