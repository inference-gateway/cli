package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecordExportAndAggregate is the end-to-end check: record into the OTel
// instruments, flush the stdout exporter to the local file on Shutdown, then
// parse it back with Aggregate. Exercises the real recording + OTLP/semconv file
// format + stats parsing in one path.
func TestRecordExportAndAggregate(t *testing.T) {
	dir := t.TempDir()
	cost := func(_ string, _, _ int) (float64, float64, float64) { return 0.01, 0.02, 0.03 }
	rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-1", Cost: cost})
	if rec == nil {
		t.Fatal("expected a recorder when enabled")
	}

	rec.RecordTool("Read", ToolError, ErrTypeTool, 12*time.Millisecond)
	rec.RecordTool("Read", ToolSuccess, "", 8*time.Millisecond)
	rec.RecordTool("Bash", ToolRejected, "", 3*time.Millisecond)
	rec.RecordUsage("deepseek/deepseek-chat", 100, 42)
	rec.RecordSession("standard", RunSuccess, time.Second)

	rec.Shutdown(context.Background()) // final export to the file

	stats, err := Aggregate(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Empty {
		t.Fatal("expected non-empty stats")
	}

	read := findTool(stats.Tools, "Read")
	if read == nil || read.Calls != 2 || read.Failures != 1 {
		t.Fatalf("Read: got %+v, want 2 calls / 1 failure", read)
	}
	if b := findTool(stats.Tools, "Bash"); b == nil || b.Failures != 0 {
		t.Fatalf("rejection must not count as a failure: %+v", b)
	}

	if len(stats.Models) != 1 {
		t.Fatalf("models=%d, want 1", len(stats.Models))
	}
	if m := stats.Models[0]; m.Total != 142 || m.Prompt != 100 || m.Completion != 42 {
		t.Fatalf("token totals wrong: %+v", m)
	}
	if stats.Models[0].Cost <= 0 {
		t.Errorf("cost not recorded: %v", stats.Models[0].Cost)
	}

	if len(stats.Sessions) != 1 {
		t.Fatalf("sessions=%d, want 1", len(stats.Sessions))
	}
	if s := stats.Sessions[0]; s.Count != 1 || s.Mode != "standard" || s.Execution != ExecHeadless {
		t.Fatalf("session wrong: %+v (execution should default to headless)", s)
	}
}

func TestAggregateEmptyStore(t *testing.T) {
	stats, err := Aggregate(t.TempDir(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Empty {
		t.Fatal("expected Empty on a fresh store")
	}
}

func TestArchiveMovesStaleFiles(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.jsonl")
	fresh := filepath.Join(dir, "fresh.jsonl")
	for _, f := range []string{old, fresh} {
		if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stale := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}

	Archive(dir, time.Now().Add(-24*time.Hour))

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("stale file should have left the active dir")
	}
	if _, err := os.Stat(filepath.Join(dir, "archive", "old.jsonl")); err != nil {
		t.Fatalf("stale file should be in archive/: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh file should stay: %v", err)
	}
}

func TestNewDisabledReturnsNil(t *testing.T) {
	if New(Options{Enabled: false, Dir: t.TempDir()}) != nil {
		t.Fatal("a disabled recorder must be nil")
	}
	var r *Recorder // nil methods must not panic
	r.RecordTool("X", ToolSuccess, "", 0)
	r.RecordUsage("m", 1, 2)
	r.RecordSession("standard", RunSuccess, 0)
	r.Shutdown(context.Background())
}

// TestSemconvValuesPinned is a cross-repo contract guard: these must equal the
// exact strings the gateway (internal/otel) and infer-action (src/otel.ts) use,
// so the exported metrics line up with existing dashboards. Changing a value is
// a breaking change across three repos.
func TestSemconvValuesPinned(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{ToolSuccess, "success"},
		{ToolError, "error"},
		{ToolRejected, "rejected"},
		{ErrTypeTool, "tool_error"},
		{RunSuccess, "success"},
		{RunFailed, "failed"},
		{RunStoppedEarly, "stopped_early"},
		{ExecInteractive, "interactive"},
		{ExecHeadless, "headless"},
	} {
		if c.got != c.want {
			t.Errorf("semconv drift: got %q, want %q", c.got, c.want)
		}
	}
}

func TestProviderFromModel(t *testing.T) {
	for in, want := range map[string]string{
		"deepseek/deepseek-chat": "deepseek",
		"gpt-4o":                 "unknown",
		"":                       "unknown",
	} {
		if got := providerFromModel(in); got != want {
			t.Errorf("providerFromModel(%q)=%q, want %q", in, got, want)
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
