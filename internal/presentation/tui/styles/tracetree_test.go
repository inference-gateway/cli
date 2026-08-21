package styles

import (
	"strings"
	"testing"

	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

func TestRenderTraceTree(t *testing.T) {
	roots := []*telemetry.TraceSpan{{
		Name:       "session",
		DurationMs: 42100,
		Attributes: map[string]string{"infer.agent.mode": "standard", "infer.run.outcome": "success"},
		Children: []*telemetry.TraceSpan{{
			Name:       "chat openai/gpt-4o",
			DurationMs: 3200,
			Children: []*telemetry.TraceSpan{{
				Name:       "execute_tool Bash",
				DurationMs: 27500,
				Error:      "tool_error",
				Attributes: map[string]string{"gen_ai.tool.call.id": "call_abc123"},
			}},
		}},
	}}
	rendered := RenderTraceTree(roots, TreeStyle{})
	for _, want := range []string{
		"session (standard, success)",
		"╰── chat openai/gpt-4o",
		"╰── execute_tool Bash call_abc123",
		"[error: tool_error]",
		"42.1s",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered tree missing %q:\n%s", want, rendered)
		}
	}
}

func TestSpanLabelServiceName(t *testing.T) {
	s := &telemetry.TraceSpan{Name: "a2a.request", Attributes: map[string]string{"service.name": "mock-agent"}}
	if got := spanLabel(s); got != "a2a.request [mock-agent]" {
		t.Fatalf("spanLabel=%q, want %q", got, "a2a.request [mock-agent]")
	}
}

func TestFormatSpanDuration(t *testing.T) {
	tests := []struct {
		ms   float64
		want string
	}{
		{0, "0µs"},
		{0.117, "117µs"},
		{9, "9ms"},
		{999, "999ms"},
		{3200, "3.2s"},
		{42100, "42.1s"},
		{92000, "1m32s"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatSpanDuration(tt.ms); got != tt.want {
				t.Fatalf("formatSpanDuration(%v)=%q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}
