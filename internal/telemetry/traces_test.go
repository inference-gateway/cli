package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadTraceTree_EndToEnd records a real session -> LLM turn -> failing
// tool trace through the Recorder and asserts the loaded tree shape, so the
// viewer stays in lock-step with the writer's format.
func TestLoadTraceTree_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-v"})
	if rec == nil {
		t.Fatal("expected a recorder when enabled")
	}

	endSession := rec.StartSession("standard")
	turnCtx, turnSpan := rec.StartLLMTurnSpan(rec.SpanContext(context.Background()), "openai/gpt-4o")
	toolCtx, toolSpan := rec.startToolSpan(turnCtx, "Bash")
	SetSpanError(toolCtx, errors.New("boom"))
	toolSpan.End()
	turnSpan.End()
	endSession(RunSuccess)
	rec.Shutdown(context.Background())

	roots, err := LoadTraceTree(dir, "sess-v")
	if err != nil {
		t.Fatalf("LoadTraceTree: %v", err)
	}
	if len(roots) != 1 || roots[0].Name != "session" {
		t.Fatalf("roots=%+v, want single session root", roots)
	}
	session := roots[0]
	if len(session.Children) != 1 || session.Children[0].Name != "chat openai/gpt-4o" {
		t.Fatalf("session children=%+v, want one chat span", session.Children)
	}
	turn := session.Children[0]
	if len(turn.Children) != 1 || turn.Children[0].Name != "execute_tool Bash" {
		t.Fatalf("turn children=%+v, want one tool span", turn.Children)
	}
	if turn.Children[0].Error == "" {
		t.Fatal("failed tool span must carry an error")
	}

	rendered := RenderTraceTree(roots, TreeStyle{})
	for _, want := range []string{"session (standard, success)", "╰── chat openai/gpt-4o", "╰── execute_tool Bash", "[error:"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered tree missing %q:\n%s", want, rendered)
		}
	}
}

func TestLoadTraceTree(t *testing.T) {
	line := func(name, spanID, parentID, start, end, status string) string {
		return `{"Name":"` + name + `","StartTime":"` + start + `","EndTime":"` + end +
			`","SpanContext":{"SpanID":"` + spanID + `"},"Parent":{"SpanID":"` + parentID +
			`"},"Status":{"Code":"` + status + `","Description":""}}`
	}
	tests := []struct {
		name      string
		lines     []string
		wantRoots []string
		wantErr   bool
	}{
		{
			name: "children sorted by start time under parent",
			lines: []string{
				line("late", "cc", "aa", "2026-01-01T00:00:05Z", "2026-01-01T00:00:06Z", "Unset"),
				line("early", "bb", "aa", "2026-01-01T00:00:01Z", "2026-01-01T00:00:02Z", "Unset"),
				line("session", "aa", "0000000000000000", "2026-01-01T00:00:00Z", "2026-01-01T00:00:10Z", "Unset"),
			},
			wantRoots: []string{"session"},
		},
		{
			name: "orphans grouped under a synthetic in-progress root",
			lines: []string{
				line("chat m", "bb", "ffff", "2026-01-01T00:00:01Z", "2026-01-01T00:00:02Z", "Unset"),
				line("execute_tool Read", "cc", "ffff", "2026-01-01T00:00:03Z", "2026-01-01T00:00:04Z", "Unset"),
			},
			wantRoots: []string{"session (in progress)"},
		},
		{
			name: "malformed lines skipped",
			lines: []string{
				"not json",
				line("session", "aa", "0000000000000000", "2026-01-01T00:00:00Z", "2026-01-01T00:00:10Z", "Unset"),
			},
			wantRoots: []string{"session"},
		},
		{
			name: "duplicate span IDs deduped keeping first",
			lines: []string{
				line("session", "aa", "0000000000000000", "2026-01-01T00:00:00Z", "2026-01-01T00:00:10Z", "Unset"),
				line("session-echo", "aa", "0000000000000000", "2026-01-01T00:00:00Z", "2026-01-01T00:00:10Z", "Unset"),
			},
			wantRoots: []string{"session"},
		},
		{
			name:    "missing file errors",
			lines:   nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.lines != nil {
				content := strings.Join(tt.lines, "\n") + "\n"
				if err := os.WriteFile(filepath.Join(dir, "s1-traces.jsonl"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			roots, err := LoadTraceTree(dir, "s1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			var names []string
			for _, r := range roots {
				names = append(names, r.Name)
			}
			if strings.Join(names, ",") != strings.Join(tt.wantRoots, ",") {
				t.Fatalf("roots=%v, want %v", names, tt.wantRoots)
			}
			if tt.name == "children sorted by start time under parent" {
				kids := roots[0].Children
				if len(kids) != 2 || kids[0].Name != "early" || kids[1].Name != "late" {
					t.Fatalf("children=%+v, want early then late", kids)
				}
			}
		})
	}
}

func TestSpanLabelServiceName(t *testing.T) {
	s := &TraceSpan{Name: "a2a.request", Attributes: map[string]string{"service.name": "mock-agent"}}
	if got := spanLabel(s); got != "a2a.request [mock-agent]" {
		t.Fatalf("spanLabel=%q, want %q", got, "a2a.request [mock-agent]")
	}
}

func TestLoadTraceTree_ErrorStatus(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantError string
	}{
		{
			name:      "error.type wins",
			line:      `{"Name":"s","SpanContext":{"SpanID":"aa"},"Parent":{"SpanID":"00"},"Attributes":[{"Key":"error.type","Value":{"Value":"tool_error"}}],"Status":{"Code":"Error","Description":"boom"}}`,
			wantError: "tool_error",
		},
		{
			name:      "status description fallback",
			line:      `{"Name":"s","SpanContext":{"SpanID":"aa"},"Parent":{"SpanID":"00"},"Status":{"Code":"Error","Description":"boom"}}`,
			wantError: "boom",
		},
		{
			name:      "unset status is not an error",
			line:      `{"Name":"s","SpanContext":{"SpanID":"aa"},"Parent":{"SpanID":"00"},"Status":{"Code":"Unset","Description":""}}`,
			wantError: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "s1-traces.jsonl"), []byte(tt.line+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			roots, err := LoadTraceTree(dir, "s1")
			if err != nil || len(roots) != 1 {
				t.Fatalf("roots=%v err=%v, want one root", roots, err)
			}
			if roots[0].Error != tt.wantError {
				t.Fatalf("Error=%q, want %q", roots[0].Error, tt.wantError)
			}
		})
	}
}

func TestTraceSessions(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, content string, mtime time.Time) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Now().Add(-time.Hour)
	write("old-traces.jsonl", "{}\n", base)
	write("new-traces.jsonl", "{}\n", base.Add(time.Minute))
	write("empty-traces.jsonl", "", base)
	write("metrics.jsonl", "{}\n", base) // metric file, not a trace file

	sessions, err := TraceSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range sessions {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "new,old" {
		t.Fatalf("sessions=%v, want [new old] (newest first, empty and metric files skipped)", ids)
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
