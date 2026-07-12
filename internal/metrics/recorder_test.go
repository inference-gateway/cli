package metrics

import (
	"testing"
	"time"
)

func TestRecordAndAggregate(t *testing.T) {
	dir := t.TempDir()
	rec := New(dir, true)
	if rec == nil {
		t.Fatal("expected a recorder when enabled")
	}

	rec.RecordTool("Read", ToolError, errTypeTool, 12*time.Millisecond)
	rec.RecordTool("Read", ToolSuccess, "", 8*time.Millisecond)
	rec.RecordTool("Bash", ToolRejected, "", 3*time.Millisecond)
	rec.RecordUsage("deepseek/deepseek-chat", 100, 42)
	rec.RecordSessionStart("s1", "standard")
	rec.RecordSessionEnd("s1", "standard", time.Second, RunSuccess)

	stats, err := Aggregate(dir, time.Time{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Empty {
		t.Fatal("expected non-empty stats")
	}

	read := findTool(stats.Tools, "Read")
	if read == nil {
		t.Fatal("no Read tool stat")
	}
	// 2 Read calls, exactly 1 failure (the rejection is a Bash call, not a failure).
	if read.Calls != 2 || read.Failures != 1 {
		t.Fatalf("Read: got calls=%d failures=%d, want 2/1", read.Calls, read.Failures)
	}
	if bash := findTool(stats.Tools, "Bash"); bash == nil || bash.Failures != 0 {
		t.Fatalf("rejection must not count as a failure: %+v", bash)
	}

	// One model, 100 + 42 = 142 tokens.
	if len(stats.Models) != 1 {
		t.Fatalf("models=%d, want 1", len(stats.Models))
	}
	m := stats.Models[0]
	if m.Total != 142 || m.Prompt != 100 || m.Completion != 42 {
		t.Fatalf("token totals wrong: %+v", m)
	}

	// One complete session in standard mode.
	if len(stats.Modes) != 1 {
		t.Fatalf("modes=%d, want 1", len(stats.Modes))
	}
	if got := stats.Modes[0]; got.Mode != "standard" || got.Count != 1 || got.Incomplete != 0 {
		t.Fatalf("mode stat wrong: %+v", got)
	}
}

func TestAggregateEmptyStore(t *testing.T) {
	stats, err := Aggregate(t.TempDir(), time.Time{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Empty {
		t.Fatal("expected Empty on a fresh store")
	}
	if len(stats.Tools)+len(stats.Models)+len(stats.Modes) != 0 {
		t.Fatal("expected no rows")
	}
}

func TestIncompleteSession(t *testing.T) {
	dir := t.TempDir()
	rec := New(dir, true)
	rec.RecordSessionStart("crashed", "plan") // start, no end

	stats, err := Aggregate(dir, time.Time{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Modes) != 1 || stats.Modes[0].Incomplete != 1 || stats.Modes[0].Count != 1 {
		t.Fatalf("expected 1 incomplete plan session, got %+v", stats.Modes)
	}
}

// TestNewDisabledReturnsNil pins the invariant the container relies on for
// "zero overhead when disabled": a disabled recorder is nil, so the container
// skips wrapping the tool service, and every method is a nil-safe no-op.
func TestNewDisabledReturnsNil(t *testing.T) {
	if New(t.TempDir(), false) != nil {
		t.Fatal("a disabled recorder must be nil")
	}
	var r *Recorder // methods must not panic on nil
	r.RecordTool("X", ToolSuccess, "", 0)
	r.RecordUsage("m", 1, 2)
	r.RecordSessionStart("s", "standard")
	r.RecordSessionEnd("s", "standard", 0, RunSuccess)
}

func TestPercentile(t *testing.T) {
	durs := []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	if got := percentile(durs, 50); got != 50 {
		t.Errorf("p50=%d, want 50", got)
	}
	if got := percentile(durs, 95); got != 100 {
		t.Errorf("p95=%d, want 100", got)
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile of empty=%d, want 0", got)
	}
}

// TestOTelEnumValuesPinned is a cross-repo contract guard: these must equal the
// exact strings the gateway (internal/otel) and infer-action (src/otel.ts) use,
// so the OTLP exporter this unblocks is a 1:1 projection. Changing a value here
// is a breaking change across three repos.
func TestOTelEnumValuesPinned(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{ToolSuccess, "success"},    // infer.tool.outcome
		{ToolError, "error"},        // infer.tool.outcome
		{ToolRejected, "rejected"},  // CLI extension
		{errTypeTool, "tool_error"}, // error.type
		{RunSuccess, "success"},     // infer.run.outcome
		{RunFailed, "failed"},       // infer.run.outcome
		{RunStoppedEarly, "stopped_early"},
	} {
		if c.got != c.want {
			t.Errorf("OTel enum drift: got %q, want %q", c.got, c.want)
		}
	}
}

func findTool(tools []ToolStat, name string) *ToolStat {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}
