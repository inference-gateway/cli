package headless

import (
	"encoding/json"
	"strings"
)

// FormatAgentMessage parses a JSON line from the agent's stdout and returns
// a human-readable message to send to the channel. Returns empty string for
// messages that should not be forwarded (status messages, tool results, etc.).
func FormatAgentMessage(line []byte) string {
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
		result := strings.TrimSpace(content)
		if result == "" {
			return ""
		}
		if r := []rune(result); len(r) > maxToolResultLen {
			result = string(r[:maxToolResultLen]) + "…"
		}
		if failed, _ := msg["failed"].(bool); failed {
			return quoteBlock("Tool failed - retrying may follow:\n```\n" + result + "\n```")
		}
		return quoteBlock("```\n" + result + "\n```")
	}

	return ""
}

// maxToolResultLen caps how much of a tool result is forwarded to the channel
// so a large file read or command output doesn't flood the chat.
const maxToolResultLen = 1000

// quoteBlock prefixes every line with "> " so tool traffic arrives as a
// markdown blockquote. Channels render quotes as collapsed/secondary content
// (Telegram: <blockquote expandable>).
func quoteBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	return strings.Join(lines, "\n")
}
