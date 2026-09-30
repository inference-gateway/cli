package computer

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// PublishedEvent maps the computer context's chat events into the run state and
// activity they publish: the screenRecording key and the computer-use actions a
// client renders live. A paused or resumed computer use ends and starts runs
// instead, which clients see through the run frames, so it publishes nothing.
func PublishedEvent(event agentdomain.ChatEvent) (agui.Published, bool) {
	switch ev := event.(type) {
	case agentdomain.ScreenRecordingStatusEvent:
		state := map[string]any{"active": ev.Active}
		if ev.Path != "" {
			state["path"] = ev.Path
			state["region"] = map[string]any{"x": ev.RegionX, "y": ev.RegionY, "width": ev.RegionWidth, "height": ev.RegionHeight}
			state["frameWidth"] = ev.FrameWidth
			state["frameHeight"] = ev.FrameHeight
		}
		return agui.Published{StateKey: agui.StateScreenRecording, StateValue: state}, true
	case agentdomain.ComputerUseActionEvent:
		content := map[string]any{
			"toolCallId":   ev.ToolCallID,
			"action":       ev.Action,
			"screenWidth":  ev.ScreenWidth,
			"screenHeight": ev.ScreenHeight,
		}
		if aimsPointer(ev.Action) {
			content["x"], content["y"] = ev.X, ev.Y
		}
		return agui.Published{ActivityType: "computer_use", MessageID: "computer_use:" + ev.ToolCallID, Content: content}, true
	}
	return agui.Published{}, false
}

// aimsPointer reports whether the action positions the pointer, so its event
// carries the scaled coordinates. Keyboard actions have none.
func aimsPointer(action string) bool {
	switch actionKinds[action] {
	case computerdomain.ActionMove, computerdomain.ActionClick, computerdomain.ActionDoubleClick, computerdomain.ActionTripleClick:
		return true
	}
	return false
}
