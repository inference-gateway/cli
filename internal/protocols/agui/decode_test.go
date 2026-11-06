package agui

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestDecodeReadsBackEveryEventARunWrites(t *testing.T) {
	w := &writes{}
	run := NewRun(w)
	run.Start("thread-1", "run-1")
	run.Snapshot([]Message{{ID: "m0", Role: "user"}})
	run.Reasoning("thinking")
	run.Text("hello")
	run.CloseMessage()
	run.ToolCall("call-1", "Bash", `{"command":"ls"}`)
	run.ToolResult("call-1", "ok")
	run.PatchState(StateTodos, []any{})
	run.Activity("tool:call-1", "tool_progress", map[string]any{"status": "running"})
	if err := run.Finish(nil, nil); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	WriteCustom(w, "approval_request", map[string]any{"tool_call_id": "call-2"})

	failed := NewRun(w)
	failed.Start("thread-1", "run-2")
	failed.Fail(errors.New("boom"))
	_ = failed.Finish(nil, nil)

	var decoded []string
	for _, line := range w.lines {
		event, err := Decode([]byte(line))
		if err != nil {
			t.Fatalf("Decode(%s): %v", line, err)
		}
		decoded = append(decoded, fmt.Sprintf("%T", event))
	}

	for _, want := range []Event{
		&RunStartedEvent{}, &RunFinishedEvent{}, &RunErrorEvent{},
		&TextMessageStartEvent{}, &TextMessageContentEvent{}, &TextMessageEndEvent{},
		&ReasoningMessageStartEvent{}, &ReasoningMessageContentEvent{}, &ReasoningMessageEndEvent{},
		&ToolCallStartEvent{}, &ToolCallArgsEvent{}, &ToolCallEndEvent{}, &ToolCallResultEvent{},
		&StateSnapshotEvent{}, &StateDeltaEvent{}, &MessagesSnapshotEvent{},
		&ActivitySnapshotEvent{}, &CustomEvent{},
	} {
		if name := fmt.Sprintf("%T", want); !slices.Contains(decoded, name) {
			t.Errorf("no written event decoded as %s, got %v", name, decoded)
		}
	}
}

func TestDecodeRefusesAnAppFrame(t *testing.T) {
	if event, err := Decode([]byte(`{"type":"issues","issues":[]}`)); err == nil {
		t.Fatalf("Decode of an app frame = %T, want an error", event)
	}
}
