package agui

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// writes records every Write call, so tests can assert one event per Write.
type writes struct {
	lines []string
}

func (w *writes) Write(p []byte) (int, error) {
	w.lines = append(w.lines, string(p))
	return len(p), nil
}

// wireEvent is the slice of an AG-UI event the tests assert on.
type wireEvent struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	RunID   string `json:"runId"`
	Outcome struct {
		Type string `json:"type"`
	} `json:"outcome"`
	Result map[string]any `json:"result"`
}

func decode(t *testing.T, w *writes) []wireEvent {
	t.Helper()
	events := make([]wireEvent, 0, len(w.lines))
	for _, line := range w.lines {
		if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
			t.Fatalf("write %q must carry exactly one newline-terminated event", line)
		}
		var ev wireEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("write is not one JSON event: %v\n%s", err, line)
		}
		events = append(events, ev)
	}
	return events
}

func types(events []wireEvent) []string {
	got := make([]string, 0, len(events))
	for _, ev := range events {
		got = append(got, ev.Type)
	}
	return got
}

func TestRunFramesMessages(t *testing.T) {
	tests := []struct {
		name  string
		write func(r *Run)
		want  []string
	}{
		{
			name:  "text opens and closes one message",
			write: func(r *Run) { r.Text("a"); r.Text("b") },
			want:  []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"},
		},
		{
			name:  "text closes the reasoning before it",
			write: func(r *Run) { r.Reasoning("think"); r.Text("say") },
			want: []string{
				"RUN_STARTED", "REASONING_MESSAGE_START", "REASONING_MESSAGE_CONTENT", "REASONING_MESSAGE_END",
				"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED",
			},
		},
		{
			name:  "reasoning after text starts a new message",
			write: func(r *Run) { r.Text("say"); r.Reasoning("think") },
			want: []string{
				"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
				"REASONING_MESSAGE_START", "REASONING_MESSAGE_CONTENT", "REASONING_MESSAGE_END", "RUN_FINISHED",
			},
		},
		{
			name:  "a user message closes what the assistant had open",
			write: func(r *Run) { r.Text("say"); r.UserMessage("hi") },
			want: []string{
				"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
				"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED",
			},
		},
		{
			name: "a tool call without arguments skips the args event",
			write: func(r *Run) {
				r.ToolCall("tc1", "Read", `{"path":"a"}`)
				r.ToolCall("tc2", "Tabs", "")
				r.ToolResult("tc1", "ok")
			},
			want: []string{
				"RUN_STARTED", "TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END",
				"TOOL_CALL_START", "TOOL_CALL_END", "TOOL_CALL_RESULT", "RUN_FINISHED",
			},
		},
		{
			name: "snapshots and custom events",
			write: func(r *Run) {
				r.Snapshot([]Message{{ID: "m1", Role: "user", Content: "hi"}})
				r.State(map[string]any{"todos": []string{}})
				r.Custom("notice", map[string]bool{"active": true})
			},
			want: []string{"RUN_STARTED", "MESSAGES_SNAPSHOT", "STATE_SNAPSHOT", "CUSTOM", "RUN_FINISHED"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writes{}
			r := NewRun(w)
			r.Start("t1", "run-1")
			tt.write(r)
			if err := r.Finish(nil); err != nil {
				t.Fatalf("Finish() err = %v", err)
			}
			if got := types(decode(t, w)); !slices.Equal(got, tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunFinishWritesOneTerminalEvent(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name        string
		write       func(r *Run)
		wantType    string
		wantOutcome string
		wantErr     error
	}{
		{"a clean run succeeds", func(*Run) {}, "RUN_FINISHED", "success", nil},
		{"a failure is a run error", func(r *Run) { r.Fail(boom) }, "RUN_ERROR", "", boom},
		{"a cancellation is a cancelled outcome", func(r *Run) { r.Fail(context.Canceled) }, "RUN_FINISHED", "cancelled", context.Canceled},
		{"a later failure replaces a cancellation", func(r *Run) { r.Fail(context.Canceled); r.Fail(boom) }, "RUN_ERROR", "", boom},
		{
			name: "a resuming event discards the cancellation",
			write: func(r *Run) {
				r.Fail(context.Canceled)
				r.Publish(CustomEvent{Name: "resumed", Resumes: true})
			},
			wantType: "RUN_FINISHED", wantOutcome: "success",
		},
		{
			name: "any other event keeps it",
			write: func(r *Run) {
				r.Fail(context.Canceled)
				r.Publish(CustomEvent{Name: "notice"})
			},
			wantType: "RUN_FINISHED", wantOutcome: "cancelled", wantErr: context.Canceled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writes{}
			r := NewRun(w)
			r.Start("t1", "run-1")
			tt.write(r)
			if err := r.Finish(nil); !errors.Is(err, tt.wantErr) {
				t.Errorf("Finish() err = %v, want %v", err, tt.wantErr)
			}
			events := decode(t, w)
			terminal := events[len(events)-1]
			if terminal.Type != tt.wantType || terminal.Outcome.Type != tt.wantOutcome {
				t.Errorf("terminal event = %s outcome %q, want %s outcome %q", terminal.Type, terminal.Outcome.Type, tt.wantType, tt.wantOutcome)
			}
			for _, ev := range events[:len(events)-1] {
				if ev.Type == "RUN_FINISHED" || ev.Type == "RUN_ERROR" {
					t.Errorf("terminal event %s before the end", ev.Type)
				}
			}
		})
	}
}

func TestRunFinishCarriesTheResult(t *testing.T) {
	w := &writes{}
	r := NewRun(w)
	r.Start("t1", "run-1")
	if err := r.Finish(map[string]any{"inputTokens": 3}); err != nil {
		t.Fatalf("Finish() err = %v", err)
	}
	events := decode(t, w)
	if got := events[len(events)-1].Result["inputTokens"]; got != float64(3) {
		t.Errorf("result inputTokens = %v, want 3", got)
	}
}

func TestRunThatNeverStartedFailsWithoutARunID(t *testing.T) {
	w := &writes{}
	r := NewRun(w)
	r.Fail(errors.New("gateway down"))
	_ = r.Finish(nil)

	events := decode(t, w)
	if len(events) != 1 || events[0].Type != "RUN_ERROR" || events[0].RunID != "" {
		t.Fatalf("events = %+v, want one RUN_ERROR without a run id", events)
	}
	if !strings.Contains(w.lines[0], "gateway down") {
		t.Errorf("RUN_ERROR lost its message: %s", w.lines[0])
	}
}
