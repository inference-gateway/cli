package telemetry

import (
	"context"
	"path/filepath"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// stubToolService embeds the interface so only the two execute entry points
// need implementing; every other method would panic if the decorator called it,
// which is exactly the assertion we want for a pass-through decorator.
type stubToolService struct {
	agentdomain.ToolService
	called string
}

func (s *stubToolService) ExecuteTool(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	s.called = "ExecuteTool"
	return &agentdomain.ToolExecutionResult{ToolName: tool.Name, Success: true}, nil
}

func (s *stubToolService) ExecuteToolDirect(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	s.called = "ExecuteToolDirect"
	return &agentdomain.ToolExecutionResult{ToolName: tool.Name, Success: true}, nil
}

// Both entry points must emit an execute_tool span: a `!!` invocation went
// through ExecuteToolDirect, which used to pass through the decorator
// uninstrumented and left `infer traces` showing a childless session.
func TestToolServiceInstrumentsBothEntryPoints(t *testing.T) {
	tests := []struct {
		name       string
		direct     bool
		wantCalled string
	}{
		{"llm driven", false, "ExecuteTool"},
		{"user typed", true, "ExecuteToolDirect"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-tool"})
			if rec == nil {
				t.Fatal("expected a recorder when enabled")
			}
			stub := &stubToolService{}
			svc := NewToolService(stub, rec)

			ctx := rec.SpanContext(context.Background())
			call := sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}

			var err error
			if tt.direct {
				_, err = svc.ExecuteToolDirect(agentdomain.WithDirectExecution(ctx), call)
			} else {
				_, err = svc.ExecuteTool(ctx, call)
			}
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			rec.Shutdown(context.Background())

			if stub.called != tt.wantCalled {
				t.Errorf("inner call = %q, want %q", stub.called, tt.wantCalled)
			}

			spans := readSpans(t, filepath.Join(dir, "sess-tool-traces.jsonl"))
			span, ok := spans["execute_tool Read"]
			if !ok {
				t.Fatalf("missing execute_tool span (got %v)", spans)
			}
			if got := span.attr("infer.tool.outcome"); got != ToolSuccess {
				t.Errorf("infer.tool.outcome = %v, want %v", got, ToolSuccess)
			}
			if got := span.attr("infer.tool.direct"); (got == true) != tt.direct {
				t.Errorf("infer.tool.direct = %v, want direct=%v", got, tt.direct)
			}
		})
	}
}
