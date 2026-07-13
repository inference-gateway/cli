package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// exportedSpan is a minimal decode of one stdouttrace span stub.
type exportedSpan struct {
	Name        string
	SpanKind    int
	SpanContext struct {
		TraceID string
		SpanID  string
	}
	Parent struct {
		TraceID string
		SpanID  string
	}
	Attributes []struct {
		Key   string
		Value struct {
			Value any
		}
	}
}

func (s exportedSpan) attr(key string) any {
	for _, a := range s.Attributes {
		if a.Key == key {
			return a.Value.Value
		}
	}
	return nil
}

const zeroSpanID = "0000000000000000"

// readSpans parses the per-session trace file (one span stub JSON object per
// line - stdouttrace encodes each span individually).
func readSpans(t *testing.T, path string) map[string]exportedSpan {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace file: %v", err)
	}
	defer func() { _ = fh.Close() }()

	spans := map[string]exportedSpan{}
	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var s exportedSpan
		if err := json.Unmarshal(scanner.Bytes(), &s); err != nil {
			t.Fatalf("unmarshal span line: %v", err)
		}
		spans[s.Name] = s
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return spans
}

// TestTraceSpansNestAndExport is the end-to-end check for the traces signal:
// session root -> LLM turn -> tool call, all in one trace, exported to the
// per-session local file.
func TestTraceSpansNestAndExport(t *testing.T) {
	dir := t.TempDir()
	rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-t"})
	if rec == nil {
		t.Fatal("expected a recorder when enabled")
	}

	endSession := rec.StartSession("standard")
	turnCtx, turnSpan := rec.StartLLMTurnSpan(rec.SpanContext(context.Background()), "openai/gpt-4o")
	SetSpanUsage(turnCtx, 100, 42)
	_, toolSpan := rec.startToolSpan(turnCtx, "Read")
	toolSpan.End()
	turnSpan.End()
	endSession(RunSuccess)

	rec.Shutdown(context.Background())

	spans := readSpans(t, filepath.Join(dir, "sess-t-traces.jsonl"))
	if len(spans) != 3 {
		t.Fatalf("spans=%d, want 3 (%v)", len(spans), spans)
	}

	session, ok := spans["session"]
	if !ok {
		t.Fatal("missing session root span")
	}
	if session.Parent.SpanID != zeroSpanID {
		t.Fatalf("session span must be a root, parent=%s", session.Parent.SpanID)
	}

	turn := spans["chat openai/gpt-4o"]
	if turn.Parent.SpanID != session.SpanContext.SpanID {
		t.Fatalf("turn parent=%s, want session %s", turn.Parent.SpanID, session.SpanContext.SpanID)
	}
	if turn.SpanKind != 3 {
		t.Fatalf("turn span kind=%d, want CLIENT (3)", turn.SpanKind)
	}
	if got := turn.attr("gen_ai.conversation.id"); got != "sess-t" {
		t.Fatalf("gen_ai.conversation.id=%v, want sess-t", got)
	}
	if in, out := turn.attr("gen_ai.usage.input_tokens"), turn.attr("gen_ai.usage.output_tokens"); in != float64(100) || out != float64(42) {
		t.Fatalf("usage attrs=(%v,%v), want (100,42)", in, out)
	}

	tool := spans["execute_tool Read"]
	if tool.Parent.SpanID != turn.SpanContext.SpanID {
		t.Fatalf("tool parent=%s, want turn %s", tool.Parent.SpanID, turn.SpanContext.SpanID)
	}
	if got := tool.attr("gen_ai.operation.name"); got != "execute_tool" {
		t.Fatalf("tool gen_ai.operation.name=%v, want execute_tool", got)
	}

	for name, s := range spans {
		if s.SpanContext.TraceID != session.SpanContext.TraceID {
			t.Fatalf("%s trace id %s, want %s (one trace per session)", name, s.SpanContext.TraceID, session.SpanContext.TraceID)
		}
	}
}

// TestNilRecorderTraceSafety: every trace entry point must be a no-op on a
// nil Recorder - no panics, contexts passed through unchanged.
func TestNilRecorderTraceSafety(t *testing.T) {
	var rec *Recorder

	end := rec.StartSession("standard")
	end(RunSuccess)

	ctx := context.Background()
	if got := rec.SpanContext(ctx); got != ctx {
		t.Fatal("SpanContext on nil recorder must return ctx unchanged")
	}

	turnCtx, turnSpan := rec.StartLLMTurnSpan(ctx, "openai/gpt-4o")
	turnSpan.End()
	if turnCtx != ctx {
		t.Fatal("StartLLMTurnSpan on nil recorder must return ctx unchanged")
	}

	toolCtx, toolSpan := rec.startToolSpan(ctx, "Read")
	toolSpan.End()
	if toolCtx != ctx {
		t.Fatal("startToolSpan on nil recorder must return ctx unchanged")
	}
}

// TestAggregateSkipsTraceFiles: a -traces.jsonl file must not feed the metric
// aggregate, even if a line in it happens to parse as metrics JSON.
func TestAggregateSkipsTraceFiles(t *testing.T) {
	metricsDir := t.TempDir()
	rec := New(Options{Enabled: true, Dir: metricsDir, SessionID: "sess-m"})
	rec.RecordTool("Read", ToolSuccess, "", 0)
	rec.Shutdown(context.Background())

	metricLine, err := os.ReadFile(filepath.Join(metricsDir, "sess-m.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sess-x-traces.jsonl"), metricLine, 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := Aggregate(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Empty {
		t.Fatalf("trace files must be skipped by Aggregate, got %+v", stats)
	}
}
