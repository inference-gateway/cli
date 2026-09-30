// The thread render: it turns one session worker's AG-UI stream into the
// channel text a chat gets, mirroring what the subprocess run forwarded
// per line - the assistant text as its message completes, the tool calls
// as quoted blocks, the results truncated, the failures as errors.

package telegram

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxToolResultLen caps how much of one tool result the chat render
// forwards so a large file read or command output cannot flood the chat.
const maxToolResultLen = 1000

// onTextStart opens or reopens the streamed message the deltas build. The
// worker echoes inbound messages; the chat never re-renders those.
func (t *threadChat) onTextStart(frame []byte) {
	var ev struct {
		Role string `json:"role"`
	}
	if json.Unmarshal(frame, &ev) != nil {
		return
	}
	t.role = ev.Role
	t.tools = nil
}

// onTextDelta appends one streamed delta of the assistant message.
func (t *threadChat) onTextDelta(frame []byte) {
	if t.role != "assistant" {
		return
	}
	var ev struct {
		Delta string `json:"delta"`
	}
	if json.Unmarshal(frame, &ev) == nil {
		t.message.WriteString(ev.Delta)
	}
}

// onTextEnd sends the completed assistant message, the way one assistant
// message arrived per subprocess line before.
func (t *threadChat) onTextEnd(frame []byte) {
	if t.role != "assistant" {
		return
	}
	t.deliverText(t.message.String())
	t.message.Reset()
	t.role = ""
}

// onToolStart opens the streamed tool call the args deltas build.
func (t *threadChat) onToolStart(frame []byte) {
	var ev struct {
		ToolCallName string `json:"toolCallName"`
	}
	if json.Unmarshal(frame, &ev) != nil {
		return
	}
	t.toolName = ev.ToolCallName
	t.toolArgs = ""
}

// onToolArgs appends one streamed arguments delta.
func (t *threadChat) onToolArgs(frame []byte) {
	var ev struct {
		Delta string `json:"delta"`
	}
	if json.Unmarshal(frame, &ev) == nil {
		t.toolArgs += ev.Delta
	}
}

// onToolEnd queues one complete tool call as the compact line the next
// render flushes.
func (t *threadChat) onToolEnd() {
	t.tools = append(t.tools, toolLine(t.toolName, t.toolArgs))
	t.toolName = ""
	t.toolArgs = ""
}

// onToolResult renders one result: the queued tool calls as the quoted
// block the subprocess render appended to the assistant message, then the
// result itself as its own quoted block.
func (t *threadChat) onToolResult(frame []byte) {
	var ev struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(frame, &ev) != nil {
		return
	}
	if quote := t.flushTools(); quote != "" {
		t.deliverText(quote)
	}
	if quote := quotedToolResult(ev.Content); quote != "" {
		t.deliverText(quote)
	}
}

// onRunError closes the turn the worker failed: anything still open, then
// the error the same way the subprocess path forwarded agent errors.
func (t *threadChat) onRunError(frame []byte) {
	var ev struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(frame, &ev) != nil || ev.Message == "" {
		return
	}
	t.deliverText(t.flushOpen() + "Error: " + ev.Message)
}

// flushOpen sends the open assistant message and the queued tool calls to
// the channel, one message each, and clears both.
func (t *threadChat) flushOpen() string {
	content := ""
	if t.message.Len() > 0 {
		content = t.message.String() + "\n\n"
		t.message.Reset()
	}
	content += t.flushTools()
	return content
}

// flushTools renders the queued tool calls as one quoted block and clears
// the queue.
func (t *threadChat) flushTools() string {
	if len(t.tools) == 0 {
		return ""
	}
	quote := quoteBlock(strings.Join(t.tools, "\n"))
	t.tools = nil
	return quote
}

// toolLine renders one tool invocation as a compact single line, e.g.
// `Bash: `+backtick+`wget -O /tmp/shot.png ...`+backtick+`. Input is the
// call's name and its streamed arguments.
func toolLine(name, args string) string {
	if args == "" {
		return name
	}
	if r := []rune(args); len(r) > maxToolResultLen {
		args = string(r[:maxToolResultLen]) + "…"
	}
	return fmt.Sprintf("%s: `%s`", name, args)
}

// quotedToolResult renders one TOOL_CALL_RESULT's content, the marshaled
// tool result, as the quoted block the subprocess render served: the error
// detail on a failure, the result otherwise, both capped.
func quotedToolResult(content string) string {
	var result struct {
		Success bool   `json:"success"`
		Result  string `json:"result"`
		Error   string `json:"error"`
	}
	failed := json.Unmarshal([]byte(content), &result) == nil && !result.Success
	if failed {
		fenced := fmt.Sprintf("Tool failed - retrying may follow:\n\"\"\n%s\n\"\"", capRunes(strings.TrimSpace(result.Error)))
		return quoteBlock(fenced)
	}
	body := strings.TrimSpace(result.Result)
	if body == "" {
		body = strings.TrimSpace(content)
	}
	if body == "" {
		return ""
	}
	return quoteBlock("\"\"\n" + capRunes(body) + "\n\"\"")
}

// capRunes caps a value at maxToolResultLen runes with an ellipsis.
func capRunes(s string) string {
	if r := []rune(s); len(r) > maxToolResultLen {
		return string(r[:maxToolResultLen]) + "…"
	}
	return s
}

// quoteBlock prefixes every line with "> " so tool traffic arrives as a
// markdown blockquote - channels render quotes as collapsed or secondary
// content (Telegram: an expandable blockquote).
func quoteBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	return strings.Join(lines, "\n")
}
