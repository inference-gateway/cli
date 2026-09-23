package telemetry

import (
	"context"
	"path/filepath"
	"testing"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
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

// failingToolService returns a Success=false result, the common tool failure
// shape (no Go error).
type failingToolService struct {
	agentdomain.ToolService
}

func (s *failingToolService) ExecuteTool(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	return &agentdomain.ToolExecutionResult{ToolName: tool.Name, Error: "old_string not found"}, nil
}

// TestToolServiceLogsFailureUnderToolSpan verifies a failed call is logged with
// the tool span's trace context and the semconv keys shared with the span.
func TestToolServiceLogsFailureUnderToolSpan(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	prev := logger.GetGlobalLogger()
	logger.SetGlobalLogger(zap.New(core))
	defer logger.SetGlobalLogger(prev)

	dir := t.TempDir()
	rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-fail"})
	svc := NewToolService(&failingToolService{}, rec)

	ctx := rec.SpanContext(context.Background())
	ctx = agentdomain.WithToolCallID(ctx, "call-1")
	ctx = agentdomain.WithSessionID(ctx, "conv-1")
	if _, err := svc.ExecuteTool(ctx, sdk.ChatCompletionMessageToolCallFunction{Name: "Edit"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	rec.Shutdown(context.Background())

	entries := logs.FilterMessage("tool call failed").AllUntimed()
	if len(entries) != 1 {
		t.Fatalf("got %d tool call failed entries, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	span := readSpans(t, filepath.Join(dir, "sess-fail-traces.jsonl"))["execute_tool Edit"]

	want := map[string]any{
		"gen_ai.tool.name":       "Edit",
		"gen_ai.tool.call.id":    "call-1",
		"gen_ai.conversation.id": "conv-1",
		"error.type":             ErrTypeTool,
		"error":                  "old_string not found",
		"trace_id":               span.SpanContext.TraceID,
		"span_id":                span.SpanContext.SpanID,
	}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("%s = %v, want %v", k, fields[k], v)
		}
	}
}

// TestToolServiceInstrumentsBothEntryPoints verifies regular and direct tool
// calls emit the same execution span.
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
