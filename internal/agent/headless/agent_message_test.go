package headless

import (
	"testing"
)

func TestFormatAgentMessage(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "assistant text message",
			line: `{"role":"assistant","content":"Hello!"}`,
			want: "Hello!",
		},
		{
			name: "tool result forwarded in code block",
			line: `{"role":"tool","content":"file contents","tool_call_id":"123"}`,
			want: "> ```\n> file contents\n> ```",
		},
		{
			name: "failed tool result is flagged",
			line: `{"role":"tool","content":"Tool execution failed: 502","tool_call_id":"123","failed":true}`,
			want: "> Tool failed - retrying may follow:\n> ```\n> Tool execution failed: 502\n> ```",
		},
		{
			name: "empty tool result is skipped",
			line: `{"role":"tool","content":"  ","tool_call_id":"123"}`,
			want: "",
		},
		{
			name: "status message is skipped",
			line: `{"type":"info","message":"Starting session"}`,
			want: "",
		},
		{
			name: "user message is skipped",
			line: `{"role":"user","content":"hello"}`,
			want: "",
		},
		{
			name: "malformed JSON is skipped",
			line: `not json`,
			want: "",
		},
		{
			name: "empty assistant content is skipped",
			line: `{"role":"assistant","content":""}`,
			want: "",
		},
		{
			name: "agent error with message",
			line: `{"type":"agent_error","message":"context length exceeded"}`,
			want: "Error: context length exceeded",
		},
		{
			name: "agent error with empty message falls back",
			line: `{"type":"agent_error","message":""}`,
			want: "Error: agent failed",
		},
		{
			name: "agent error with no message field falls back",
			line: `{"type":"agent_error"}`,
			want: "Error: agent failed",
		},
		{
			name: "notification is forwarded verbatim",
			line: `{"type":"notification","message":"✅ A2A Task Completed"}`,
			want: "✅ A2A Task Completed",
		},
		{
			name: "notification with empty message is skipped",
			line: `{"type":"notification","message":""}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAgentMessage([]byte(tt.line))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
