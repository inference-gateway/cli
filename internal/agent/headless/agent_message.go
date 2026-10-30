package headless

import (
	"encoding/json"
	"strings"

	render "github.com/inference-gateway/cli/internal/platform/render"
)

// FormatAgentMessage parses a JSON line from the agent's stdout and returns
// a human-readable message to send to the channel. Returns empty string for
// messages that should not be forwarded (status messages, tool results, etc.).
// A tool result is capped at maxToolResult runes.
func FormatAgentMessage(line []byte, maxToolResult int) string {
	var msg map[string]any
	if err := json.Unmarshal(line, &msg); err != nil {
		return ""
	}

	if t, _ := msg["type"].(string); t == "agent_error" {
		if errMsg, ok := msg["message"].(string); ok && errMsg != "" {
			return "Error: " + errMsg
		}
		return "Error: agent failed"
	}

	if t, _ := msg["type"].(string); t == "notification" {
		m, _ := msg["message"].(string)
		return m
	}

	if _, isStatus := msg["type"]; isStatus {
		return ""
	}

	role, _ := msg["role"].(string)
	content, _ := msg["content"].(string)

	switch role {
	case "assistant":
		return content
	case "tool":
		result := render.Truncate(strings.TrimSpace(content), maxToolResult)
		if result == "" {
			return ""
		}
		if failed, _ := msg["failed"].(bool); failed {
			return render.QuoteBlock("Tool failed - retrying may follow:\n```\n" + result + "\n```")
		}
		return render.QuoteBlock("```\n" + result + "\n```")
	}

	return ""
}
