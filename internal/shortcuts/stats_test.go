package shortcuts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatsShortcut_EmptyStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := NewStatsShortcut()
	res, err := s.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Fatal("expected Success to be true")
	}
	if !strings.Contains(res.Output, "No telemetry recorded yet.") {
		t.Errorf("expected 'No telemetry recorded yet.', got: %q", res.Output)
	}
}

func TestStatsShortcut_WithData(t *testing.T) {
	dir := t.TempDir()
	writeTestTelemetry(t, dir, time.Now())

	t.Setenv("HOME", dir)
	telemetryDir := filepath.Join(dir, ".infer", "telemetry")
	if err := os.MkdirAll(telemetryDir, 0o755); err != nil {
		t.Fatalf("failed to create telemetry dir: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "test.jsonl"), filepath.Join(telemetryDir, "test.jsonl")); err != nil {
		t.Fatalf("failed to move test telemetry: %v", err)
	}

	s := NewStatsShortcut()
	res, err := s.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Fatal("expected Success to be true")
	}

	if !strings.Contains(res.Output, "Tool Calls") {
		t.Errorf("expected 'Tool Calls' section, got: %q", res.Output)
	}
	if !strings.Contains(res.Output, "Token Usage") {
		t.Errorf("expected 'Token Usage' section, got: %q", res.Output)
	}
	if !strings.Contains(res.Output, "Sessions") {
		t.Errorf("expected 'Sessions' section, got: %q", res.Output)
	}

	if !strings.Contains(res.Output, "bash") {
		t.Errorf("expected 'bash' tool in output, got: %q", res.Output)
	}
}

func TestStatsShortcut_WithSince(t *testing.T) {
	dir := t.TempDir()
	writeTestTelemetry(t, dir, time.Now().AddDate(0, 0, -30))

	t.Setenv("HOME", dir)
	telemetryDir := filepath.Join(dir, ".infer", "telemetry")
	if err := os.MkdirAll(telemetryDir, 0o755); err != nil {
		t.Fatalf("failed to create telemetry dir: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "test.jsonl"), filepath.Join(telemetryDir, "test.jsonl")); err != nil {
		t.Fatalf("failed to move test telemetry: %v", err)
	}

	s := NewStatsShortcut()

	res, err := s.Execute(context.Background(), []string{"7d"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Fatal("expected Success to be true")
	}
	if !strings.Contains(res.Output, "No telemetry recorded yet.") {
		t.Errorf("expected empty store with --since 7d, got: %q", res.Output)
	}
}

func TestStatsShortcut_InvalidSince(t *testing.T) {
	s := NewStatsShortcut()
	res, err := s.Execute(context.Background(), []string{"invalid"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Success {
		t.Fatal("expected Success to be false for invalid --since")
	}
	if !strings.Contains(res.Output, "Invalid") {
		t.Errorf("expected error message, got: %q", res.Output)
	}
}

func TestStatsShortcut_CanExecute(t *testing.T) {
	s := NewStatsShortcut()
	if !s.CanExecute(nil) {
		t.Error("expected /stats to accept no arguments")
	}
	if !s.CanExecute([]string{"7d"}) {
		t.Error("expected /stats to accept one argument")
	}
	if s.CanExecute([]string{"7d", "extra"}) {
		t.Error("expected /stats to reject two non-flag arguments")
	}
	if !s.CanExecute([]string{"7d", "vertical"}) {
		t.Error("expected /stats to accept a window plus vertical")
	}
	if !s.CanExecute([]string{"7d", "table"}) {
		t.Error("expected /stats to accept a window plus table")
	}
	if !s.CanExecute([]string{"vertical"}) {
		t.Error("expected /stats to accept vertical alone")
	}
}

func TestParseStatsArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		def          bool
		wantSince    string
		wantVertical bool
	}{
		{"empty default tables", nil, false, "", false},
		{"empty default vertical", nil, true, "", true},
		{"since keeps default", []string{"24h"}, true, "24h", true},
		{"vertical forces list", []string{"vertical"}, false, "", true},
		{"table forces tables", []string{"table"}, true, "", false},
		{"tables alias", []string{"tables"}, true, "", false},
		{"since then vertical", []string{"7d", "vertical"}, false, "7d", true},
		{"vertical then since", []string{"vertical", "7d"}, false, "7d", true},
		{"since then table", []string{"7d", "table"}, true, "7d", false},
		{"case insensitive", []string{"Vertical"}, false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			since, vertical := ParseStatsArgs(tt.args, tt.def)
			if since != tt.wantSince || vertical != tt.wantVertical {
				t.Errorf("ParseStatsArgs(%v, %v) = (%q, %v), want (%q, %v)", tt.args, tt.def, since, vertical, tt.wantSince, tt.wantVertical)
			}
		})
	}
}

func TestStatsShortcut_ConversationScoped(t *testing.T) {
	dir := t.TempDir()
	writeTestTelemetry(t, dir, time.Now())

	t.Setenv("HOME", dir)
	telemetryDir := filepath.Join(dir, ".infer", "telemetry")
	if err := os.MkdirAll(telemetryDir, 0o755); err != nil {
		t.Fatalf("failed to create telemetry dir: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "test.jsonl"), filepath.Join(telemetryDir, "test.jsonl")); err != nil {
		t.Fatalf("failed to move test telemetry: %v", err)
	}

	res, err := NewStatsShortcut().WithConversation("no-such-conversation").Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(res.Output, "No telemetry recorded yet.") {
		t.Errorf("expected empty scoped stats, got: %q", res.Output)
	}

	global, err := NewStatsShortcut().Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(global.Output, "Tool Calls") {
		t.Errorf("expected global stats to include data, got: %q", global.Output)
	}
}

func TestStatsShortcut_Vertical(t *testing.T) {
	dir := t.TempDir()
	writeTestTelemetry(t, dir, time.Now())

	t.Setenv("HOME", dir)
	telemetryDir := filepath.Join(dir, ".infer", "telemetry")
	if err := os.MkdirAll(telemetryDir, 0o755); err != nil {
		t.Fatalf("failed to create telemetry dir: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "test.jsonl"), filepath.Join(telemetryDir, "test.jsonl")); err != nil {
		t.Fatalf("failed to move test telemetry: %v", err)
	}

	s := NewStatsShortcut()
	res, err := s.Execute(context.Background(), []string{"vertical"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Fatal("expected Success to be true")
	}
	if strings.Contains(res.Output, "|") {
		t.Errorf("vertical view should contain no markdown tables, got: %q", res.Output)
	}
	for _, want := range []string{"Tool Calls", "Calls:", "Token Usage", "bash"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("expected %q in vertical output, got: %q", want, res.Output)
		}
	}
}

// writeTestTelemetry writes a minimal valid OTLP stdout-exporter JSONL line
// into dir/test.jsonl with the given timestamp.
func writeTestTelemetry(t *testing.T, dir string, ts time.Time) {
	t.Helper()
	line := `{"Resource":[{"Key":"infer.execution.mode","Value":{"Value":"interactive"}}],"ScopeMetrics":[{"Metrics":[{"Name":"infer.agent.tool.calls","Data":{"DataPoints":[{"Attributes":[{"Key":"gen_ai.tool.name","Value":{"Value":"bash"}},{"Key":"infer.tool.outcome","Value":{"Value":"success"}}],"Time":"` +
		ts.Format(time.RFC3339Nano) + `","Value":1}]}},{"Name":"gen_ai.execute_tool.duration","Data":{"DataPoints":[{"Attributes":[{"Key":"gen_ai.tool.name","Value":{"Value":"bash"}}],"Time":"` +
		ts.Format(time.RFC3339Nano) + `","Sum":1.5,"Count":1}]}},{"Name":"gen_ai.client.token.usage","Data":{"DataPoints":[{"Attributes":[{"Key":"gen_ai.request.model","Value":{"Value":"openai/gpt-4o"}},{"Key":"gen_ai.token.type","Value":{"Value":"input"}}],"Time":"` +
		ts.Format(time.RFC3339Nano) + `","Sum":100,"Count":1}]}},{"Name":"infer.client.cost","Data":{"DataPoints":[{"Attributes":[{"Key":"gen_ai.request.model","Value":{"Value":"openai/gpt-4o"}}],"Time":"` +
		ts.Format(time.RFC3339Nano) + `","Value":0.002}]}},{"Name":"infer.agent.runs","Data":{"DataPoints":[{"Attributes":[{"Key":"infer.agent.mode","Value":{"Value":"auto"}}],"Time":"` +
		ts.Format(time.RFC3339Nano) + `","Value":1}]}}]}]}` + "\n"

	if err := os.WriteFile(filepath.Join(dir, "test.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatalf("failed to write test telemetry: %v", err)
	}
}
