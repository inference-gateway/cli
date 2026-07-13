package shortcuts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestTrace writes a minimal two-span session trace file for the given
// session id under home/.infer/telemetry.
func writeTestTrace(t *testing.T, home, session string) {
	t.Helper()
	dir := filepath.Join(home, ".infer", "telemetry")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"Name":"execute_tool Bash","StartTime":"2026-01-01T00:00:01Z","EndTime":"2026-01-01T00:00:03Z","SpanContext":{"SpanID":"bb"},"Parent":{"SpanID":"aa"},"Status":{"Code":"Unset","Description":""}}
{"Name":"session","StartTime":"2026-01-01T00:00:00Z","EndTime":"2026-01-01T00:00:10Z","SpanContext":{"SpanID":"aa"},"Parent":{"SpanID":"0000000000000000"},"Attributes":[{"Key":"infer.agent.mode","Value":{"Value":"standard"}},{"Key":"infer.run.outcome","Value":{"Value":"success"}}],"Status":{"Code":"Unset","Description":""}}
`
	if err := os.WriteFile(filepath.Join(dir, session+"-traces.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTracesShortcut_Execute(t *testing.T) {
	tests := []struct {
		name        string
		sessions    []string
		args        []string
		wantSuccess bool
		wantOutput  []string
	}{
		{
			name:        "empty store",
			wantSuccess: true,
			wantOutput:  []string{"No traces recorded yet."},
		},
		{
			name:        "most recent session by default",
			sessions:    []string{"sess-1"},
			wantSuccess: true,
			wantOutput:  []string{"Traces: sess-1", "session (standard, success)", "╰──", "execute_tool Bash"},
		},
		{
			name:        "explicit session id",
			sessions:    []string{"sess-1"},
			args:        []string{"sess-1"},
			wantSuccess: true,
			wantOutput:  []string{"Traces: sess-1"},
		},
		{
			name:        "unknown session id fails",
			sessions:    []string{"sess-1"},
			args:        []string{"nope"},
			wantSuccess: false,
			wantOutput:  []string{"Failed to load trace for session nope"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			for _, s := range tt.sessions {
				writeTestTrace(t, home, s)
			}

			res, err := NewTracesShortcut().Execute(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if res.Success != tt.wantSuccess {
				t.Fatalf("Success=%v, want %v (output: %q)", res.Success, tt.wantSuccess, res.Output)
			}
			for _, want := range tt.wantOutput {
				if !strings.Contains(res.Output, want) {
					t.Errorf("output missing %q, got: %q", want, res.Output)
				}
			}
		})
	}
}

func TestTracesShortcut_CanExecute(t *testing.T) {
	s := NewTracesShortcut()
	if !s.CanExecute(nil) {
		t.Error("expected /traces to accept no arguments")
	}
	if !s.CanExecute([]string{"sess-1"}) {
		t.Error("expected /traces to accept one argument")
	}
	if s.CanExecute([]string{"a", "b"}) {
		t.Error("expected /traces to reject two arguments")
	}
}
