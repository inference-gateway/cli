package render

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatAgentMessage parses a JSON line from the agent's stdout and returns
// a human-readable message to send to the channel. Returns empty string for
// messages that should not be forwarded (status messages, tool results, etc.).
func FormatAgentMessage(line []byte) string {
	var msg map[string]interface{}
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

	switch role {
	case "assistant":
		content, _ := msg["content"].(string)

		if tools, ok := msg["tools"].([]interface{}); ok && len(tools) > 0 {
			lines := make([]string, 0, len(tools))
			for _, t := range tools {
				if name, ok := t.(string); ok {
					lines = append(lines, formatToolLine(name))
				}
			}
			toolMsg := quoteBlock(strings.Join(lines, "\n"))
			if content != "" {
				return content + "\n\n" + toolMsg
			}
			return toolMsg
		}

		if content != "" {
			return content
		}

	case "tool":
		content, _ := msg["content"].(string)
		result := strings.TrimSpace(content)
		if result == "" {
			return ""
		}
		if r := []rune(result); len(r) > maxToolResultLen {
			result = string(r[:maxToolResultLen]) + "…"
		}
		if failed, _ := msg["failed"].(bool); failed {
			return quoteBlock("⚠️ Tool failed - retrying may follow:\n```\n" + result + "\n```")
		}
		return quoteBlock("```\n" + result + "\n```")
	}

	return ""
}

// maxToolResultLen caps how much of a tool result is forwarded to the channel
// so a large file read or command output doesn't flood the chat.
const maxToolResultLen = 1000

// formatToolLine renders one tool invocation as a compact single line, e.g.
// "Bash: `wget -O /tmp/shot.png …`". Input looks like "Name(args)".
func formatToolLine(tool string) string {
	name, args, found := strings.Cut(tool, "(")
	if found {
		args = strings.TrimSuffix(args, ")")
	}

	if r := []rune(args); len(r) > maxToolResultLen {
		args = string(r[:maxToolResultLen]) + "…"
	}
	if args == "" {
		return name
	}
	return fmt.Sprintf("%s: `%s`", name, args)
}

// quoteBlock prefixes every line with "> " so tool traffic arrives as a
// markdown blockquote — channels render quotes as collapsed/secondary content
// (Telegram: <blockquote expandable>).
func quoteBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	return strings.Join(lines, "\n")
}
