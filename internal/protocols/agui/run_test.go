package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
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
		Type       string          `json:"type"`
		Interrupts []wireInterrupt `json:"interrupts"`
	} `json:"outcome"`
	Result   map[string]any   `json:"result"`
	Usage    []map[string]any `json:"usage"`
	Snapshot map[string]any   `json:"snapshot"`
	// delta is the string fragment of a text or reasoning content event and,
	// on STATE_DELTA, the JSON Patch the event applies.
	Delta    json.RawMessage `json:"delta"`
	Activity struct {
		Type string `json:"activityType"`
	} `json:"activity,omitempty"`
}

type wireInterrupt struct {
	ID         string `json:"id"`
	Reason     string `json:"reason"`
	ToolCallID string `json:"toolCallId"`
}

// testdata/ag-ui-1.0-schema.json is the frozen upstream contract. Every event
// the run writer emits must validate against it, so decode compiles it once
// and checks each written line before the tests assert anything on it.
const wireSchemaID = "https://ag-ui.com/spec/1.0/schema.json"

var (
	schemaOnce sync.Once
	wireSchema *jsonschema.Schema
	schemaErr  error
)

func loadWireSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		data, err := os.ReadFile("testdata/ag-ui-1.0-schema.json")
		if err != nil {
			schemaErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			schemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		if addErr := compiler.AddResource(wireSchemaID, doc); addErr != nil {
			schemaErr = addErr
			return
		}
		wireSchema, schemaErr = compiler.Compile(wireSchemaID)
	})
	return wireSchema, schemaErr
}

func decode(t *testing.T, w *writes) []wireEvent {
	t.Helper()
	schema, err := loadWireSchema()
	if err != nil {
		t.Fatalf("loading the upstream AG-UI schema: %v", err)
	}
	events := make([]wireEvent, 0, len(w.lines))
	for _, line := range w.lines {
		if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
			t.Fatalf("write %q must carry exactly one newline-terminated event", line)
		}
		var raw any
		if uerr := json.Unmarshal([]byte(line), &raw); uerr != nil {
			t.Fatalf("write is not one JSON event: %v\n%s", uerr, line)
		}
		if err := schema.Validate(raw); err != nil {
			t.Errorf("event violates the upstream 1.0 schema: %v\n%s", err, line)
		}
		var ev wireEvent
		if uerr := json.Unmarshal([]byte(line), &ev); uerr != nil {
			t.Fatalf("write is not one JSON event: %v\n%s", uerr, line)
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

func TestRunFramesEvents(t *testing.T) {
	tests := []struct {
		name  string
		write func(r *Run)
		want  []string
	}{
		{
			name:  "text opens and closes one message",
			write: func(r *Run) { r.Text("a"); r.Text("b") },
			want:  []string{"RUN_STARTED", "STATE_SNAPSHOT", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"},
		},
		{
			name:  "text closes the reasoning before it",
			write: func(r *Run) { r.Reasoning("think"); r.Text("say") },
			want: []string{
				"RUN_STARTED", "STATE_SNAPSHOT", "REASONING_MESSAGE_START", "REASONING_MESSAGE_CONTENT", "REASONING_MESSAGE_END",
				"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED",
			},
		},
		{
			name:  "reasoning after text starts a new message",
			write: func(r *Run) { r.Text("say"); r.Reasoning("think") },
			want: []string{
				"RUN_STARTED", "STATE_SNAPSHOT", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
				"REASONING_MESSAGE_START", "REASONING_MESSAGE_CONTENT", "REASONING_MESSAGE_END", "RUN_FINISHED",
			},
		},
		{
			name:  "a queued note is its own message with its role",
			write: func(r *Run) { r.Text("say"); r.Message(Role("user"), "note") },
			want: []string{
				"RUN_STARTED", "STATE_SNAPSHOT", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
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
				"RUN_STARTED", "STATE_SNAPSHOT", "TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END",
				"TOOL_CALL_START", "TOOL_CALL_END", "TOOL_CALL_RESULT", "RUN_FINISHED",
			},
		},
		{
			name: "a state change patches one key in a delta",
			write: func(r *Run) {
				r.PatchState(StateTodos, []string{"a"})
				r.PatchState(StateUsage, map[string]any{"input": 1})
			},
			want: []string{"RUN_STARTED", "STATE_SNAPSHOT", "STATE_DELTA", "STATE_DELTA", "RUN_FINISHED"},
		},
		{
			name:  "an activity is its own snapshot",
			write: func(r *Run) { r.Activity("judge:Bash:1", "judge_verdict", map[string]string{"decision": "approved"}) },
			want:  []string{"RUN_STARTED", "STATE_SNAPSHOT", "ACTIVITY_SNAPSHOT", "RUN_FINISHED"},
		},
		{
			name:  "a messages snapshot lands inside the run",
			write: func(r *Run) { r.Snapshot([]Message{{ID: "m1", Role: "user", Content: "hi"}}) },
			want:  []string{"RUN_STARTED", "STATE_SNAPSHOT", "MESSAGES_SNAPSHOT", "RUN_FINISHED"},
		},
		{
			name:  "an event before the run starts is dropped",
			write: func(r *Run) { /* nothing reaches the run that never started */ },
			want:  []string{"RUN_STARTED", "STATE_SNAPSHOT", "RUN_FINISHED"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writes{}
			r := NewRun(w)
			if tt.name == "an event before the run starts is dropped" {
				r.Text("early")
			}
			r.Start("t1", "run-1")
			tt.write(r)
			if err := r.Finish(nil, nil); err != nil {
				t.Fatalf("Finish() err = %v", err)
			}
			events := decode(t, w)
			if sn := events[1]; sn.Type == "STATE_SNAPSHOT" {
				for _, key := range []string{StateTodos, StateUsage, StateBackgroundTasks, StateScreenRecording} {
					if _, ok := sn.Snapshot[key]; !ok {
						t.Errorf("STATE_SNAPSHOT misses the %q key", key)
					}
				}
			}
			if got := types(events); !slices.Equal(got, tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunSuspendsWithTheInterruptOutcome(t *testing.T) {
	w := &writes{}
	r := NewRun(w)
	r.Start("t1", "run-1")
	r.Suspend([]Interrupt{{ID: "call_1", Reason: InterruptToolCall, ToolCallID: "call_1"}})
	_ = r.Finish(nil, nil)

	events := decode(t, w)
	terminal := events[len(events)-1]
	if terminal.Type != "RUN_FINISHED" || terminal.Outcome.Type != "interrupt" {
		t.Fatalf("terminal event = %s outcome %q, want RUN_FINISHED outcome interrupt", terminal.Type, terminal.Outcome.Type)
	}
	if len(terminal.Outcome.Interrupts) != 1 || terminal.Outcome.Interrupts[0].ToolCallID != "call_1" ||
		terminal.Outcome.Interrupts[0].Reason != InterruptToolCall {
		t.Errorf("interrupts = %+v, want one tool_call interrupt for call_1", terminal.Outcome.Interrupts)
	}
	w.lines = nil
	r.Text("late")
	if err := r.Finish(nil, nil); err != nil {
		t.Fatalf("Finish() after Suspend err = %v", err)
	}
	if events := decode(t, w); len(events) != 0 {
		t.Errorf("an ended run wrote %d more events, want none", len(events))
	}
}

func TestRunFinishWritesOneTerminalEvent(t *testing.T) {
	boom := errors.New("boom")
	usage := []TokenUsage{{Model: "m", InputTokens: TokenCount(1), OutputTokens: TokenCount(2)}}
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writes{}
			r := NewRun(w)
			r.Start("t1", "run-1")
			tt.write(r)
			if err := r.Finish(nil, usage); !errors.Is(err, tt.wantErr) {
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

func TestRunTerminalCarriesUsageAndResult(t *testing.T) {
	usage := []TokenUsage{{Model: "m", InputTokens: TokenCount(3), OutputTokens: TokenCount(5)}}
	tests := []struct {
		name      string
		err       error
		result    map[string]any
		wantUsage bool
	}{
		{"success carries usage and result", nil, map[string]any{"cost": 0.3}, true},
		{"cancellation carries usage", context.Canceled, nil, true},
		{"failure carries usage", errors.New("boom"), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writes{}
			r := NewRun(w)
			r.Start("t1", "run-1")
			if tt.err != nil {
				r.Fail(tt.err)
			}
			_ = r.Finish(tt.result, usage)
			events := decode(t, w)
			terminal := events[len(events)-1]
			if tt.wantUsage && len(terminal.Usage) != 1 {
				t.Errorf("terminal usage = %+v, want one entry", terminal.Usage)
			}
			if tt.result != nil && terminal.Result["cost"] != 0.3 {
				t.Errorf("terminal result = %+v, want the cost in result", terminal.Result)
			}
		})
	}
}

func TestRunFinishWithoutUsageWritesNoUsage(t *testing.T) {
	w := &writes{}
	r := NewRun(w)
	r.Start("t1", "run-1")
	_ = r.Finish(nil, nil)
	events := decode(t, w)
	if events[len(events)-1].Usage != nil {
		t.Errorf("terminal event carried an empty usage array, want no usage field")
	}
}

func TestRunContinuationCarriesTheState(t *testing.T) {
	w := &writes{}
	r := NewRun(w)
	r.Start("t1", "run-1")
	r.PatchState(StateTodos, []any{map[string]any{"id": "1"}})
	r.PatchState(StateUsage, []TokenUsage{{Model: "m", InputTokens: TokenCount(1)}})

	next := NewContinuationRun(w, r.State())
	next.Start("t1", "run-2")

	events := decode(t, w)
	snapshot := events[len(events)-1]
	if snapshot.Type != "STATE_SNAPSHOT" {
		t.Fatalf("continuation opens with %s, want a seeded STATE_SNAPSHOT", snapshot.Type)
	}
	for _, key := range []string{StateTodos, StateUsage, StateBackgroundTasks, StateScreenRecording} {
		if _, ok := snapshot.Snapshot[key]; !ok {
			t.Errorf("continuation STATE_SNAPSHOT misses the %q key", key)
		}
	}
	if len(snapshot.Snapshot[StateTodos].([]any)) != 1 {
		t.Errorf("continuation lost the carried todos: %+v", snapshot.Snapshot[StateTodos])
	}
}

func TestRunThatNeverStartedFailsWithoutARunID(t *testing.T) {
	w := &writes{}
	r := NewRun(w)
	r.Fail(errors.New("gateway down"))
	_ = r.Finish(nil, nil)

	events := decode(t, w)
	if len(events) != 1 || events[0].Type != "RUN_ERROR" || events[0].RunID != "" {
		t.Fatalf("events = %+v, want one RUN_ERROR without a run id", events)
	}
	if !strings.Contains(w.lines[0], "gateway down") {
		t.Errorf("RUN_ERROR lost its message: %s", w.lines[0])
	}
}
