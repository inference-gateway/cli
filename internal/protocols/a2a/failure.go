package a2a

import (
	"strings"

	adk "github.com/inference-gateway/adk/types"
)

// textFromParts concatenates Text parts from an ADK message parts slice.
func textFromParts(parts []adk.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Text != nil {
			b.WriteString(*p.Text)
		}
	}
	return b.String()
}

// errorFromDataParts returns the first non-empty "error" string value found
// in any data part of parts, or "".
func errorFromDataParts(parts []adk.Part) string {
	for _, p := range parts {
		if e, ok := p.DataMap()["error"].(string); ok && e != "" {
			return e
		}
	}
	return ""
}

// failureReasonFromTask returns the best-available human-readable failure
// description for a task: Status.Message text → Status.Message "error"
// data-part → last agent History message text or "error" data-part. Returns
// "" when nothing is found.
//
// This is defensive against ADK servers that surface errors via different
// channels: status message, message data parts, or history.
func failureReasonFromTask(task adk.Task) string {
	if task.Status.Message != nil {
		if text := textFromParts(task.Status.Message.Parts); text != "" {
			return text
		}
		if errText := errorFromDataParts(task.Status.Message.Parts); errText != "" {
			return errText
		}
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		msg := task.History[i]
		if msg.Role != adk.RoleAgent {
			continue
		}
		if text := textFromParts(msg.Parts); text != "" {
			return text
		}
		if errText := errorFromDataParts(msg.Parts); errText != "" {
			return errText
		}
	}
	return ""
}
