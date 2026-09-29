package agui

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// writeRecordingWriter records every Write call, so tests can assert the
// encoder emits one event per Write.
type writeRecordingWriter struct {
	writes []string
}

func (w *writeRecordingWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, string(p))
	return len(p), nil
}

// wireEvent is the slice of an AG-UI event the run lifecycle tests assert on.
type wireEvent struct {
	Type    string `json:"type"`
	Outcome struct {
		Type string `json:"type"`
	} `json:"outcome"`
}

func decodeEvents(t *testing.T, out string) []wireEvent {
	t.Helper()
	var events []wireEvent
	for line := range strings.Lines(out) {
		var ev wireEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line is not one JSON event: %v\n%s", err, line)
		}
		events = append(events, ev)
	}
	return events
}

func TestRunEncoder_FinishEmitsOneTerminalEvent(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name        string
		events      []agentdomain.ChatEvent
		wantType    string
		wantOutcome string
		wantErr     error
	}{
		{
			name:        "completed run succeeds",
			events:      []agentdomain.ChatEvent{agentdomain.ChatCompleteEvent{}},
			wantType:    "RUN_FINISHED",
			wantOutcome: "success",
		},
		{
			name:        "cancelled turn finishes cancelled",
			events:      []agentdomain.ChatEvent{agentdomain.ChatChunkEvent{Content: "partial"}, agentdomain.ChatCompleteEvent{Cancelled: true}},
			wantType:    "RUN_FINISHED",
			wantOutcome: "cancelled",
			wantErr:     context.Canceled,
		},
		{
			name: "computer-use resume clears the cancellation",
			events: []agentdomain.ChatEvent{
				agentdomain.ChatCompleteEvent{Cancelled: true},
				agentdomain.ComputerUseResumedEvent{RequestID: "s1"},
				agentdomain.ChatCompleteEvent{},
			},
			wantType:    "RUN_FINISHED",
			wantOutcome: "success",
		},
		{
			name:     "failure after a cancelled turn is a run error",
			events:   []agentdomain.ChatEvent{agentdomain.ChatCompleteEvent{Cancelled: true}, agentdomain.ChatErrorEvent{Error: boom}},
			wantType: "RUN_ERROR",
			wantErr:  boom,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			r := NewRunEncoder(&out, "m", &convmocks.FakeConversationRepository{}, nil, nil, nil, nil)
			r.Start("s1", "run-1")
			for _, ev := range tt.events {
				r.Handle(ev)
			}
			if err := r.Finish(); !errors.Is(err, tt.wantErr) {
				t.Errorf("Finish() err = %v, want %v", err, tt.wantErr)
			}
			events := decodeEvents(t, out.String())
			terminal := events[len(events)-1]
			if terminal.Type != tt.wantType || terminal.Outcome.Type != tt.wantOutcome {
				t.Errorf("terminal event = %s outcome %q, want %s outcome %q\n%s", terminal.Type, terminal.Outcome.Type, tt.wantType, tt.wantOutcome, out.String())
			}
			for _, ev := range events[:len(events)-1] {
				if ev.Type == "RUN_FINISHED" || ev.Type == "RUN_ERROR" {
					t.Errorf("terminal event %s before the end\n%s", ev.Type, out.String())
				}
			}
		})
	}
}

func TestRunEncoder_StartSnapshotsRestoredHistory(t *testing.T) {
	history := []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("run the checks")}},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("working on it")}},
	}
	tests := []struct {
		name    string
		history []convdomain.ConversationEntry
		want    []string
	}{
		{"resumed run snapshots right after RUN_STARTED", history, []string{"RUN_STARTED", "MESSAGES_SNAPSHOT"}},
		{"fresh run emits no snapshot", nil, []string{"RUN_STARTED"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			NewRunEncoder(&out, "m", &convmocks.FakeConversationRepository{}, tt.history, nil, nil, nil).Start("s1", "run-1")
			var got []string
			for _, ev := range decodeEvents(t, out.String()) {
				got = append(got, ev.Type)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunEncoder_EmitApprovalResolved(t *testing.T) {
	var out strings.Builder
	r := &RunEncoder{w: &out}
	r.EmitApprovalResolved("tc1")
	got := out.String()
	for _, want := range []string{`"type":"CUSTOM"`, `"name":"approval_resolved"`, `"tool_call_id":"tc1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestRunEncoder_EmitsOneWritePerEvent(t *testing.T) {
	w := &writeRecordingWriter{}
	r := &RunEncoder{w: w}
	r.Start("s1", "run-1")
	r.Handle(agentdomain.ChatChunkEvent{Content: "hello"})
	r.Handle(agentdomain.ChatCompleteEvent{})
	if err := r.Finish(); err != nil {
		t.Fatalf("Finish() err = %v", err)
	}
	var got []string
	for _, write := range w.writes {
		events := decodeEvents(t, write)
		if len(events) != 1 || !strings.HasSuffix(write, "\n") {
			t.Fatalf("write %q must carry exactly one newline-terminated event", write)
		}
		got = append(got, events[0].Type)
	}
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if !slices.Equal(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
}

func TestSnapshotMessages(t *testing.T) {
	toolCalls := []sdk.ChatCompletionMessageToolCall{
		{ID: "tc1", Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{}`}},
	}
	toolCallID := "tc1"
	entries := []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hi")}},
		{Message: sdk.Message{Role: sdk.Assistant, ToolCalls: &toolCalls}},
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("out"), ToolCallID: &toolCallID},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolCallID: "tc1", Success: true},
		},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("system reminder")}, Hidden: true},
	}
	messages := snapshotMessages(entries)
	if len(messages) != 3 {
		t.Fatalf("snapshotMessages() = %d messages, want 3 (hidden entry skipped)", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hi" || messages[0].ID == "" {
		t.Errorf("user message = %+v, want role user with content and an id", messages[0])
	}
	if assistant := messages[1]; assistant.Role != "assistant" || assistant.ToolCallID != "" || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Function.Name != "Bash" {
		t.Errorf("assistant message = %+v, want role assistant with the stored Bash call", assistant)
	}
	if tool := messages[2]; tool.ToolCallID != "tc1" || tool.Error != "" {
		t.Errorf("successful tool message = %+v, want toolCallId tc1 without error", tool)
	}

	entries[2].ToolExecution = &agentdomain.ToolExecutionResult{ToolCallID: "tc1", Success: false, Error: "boom"}
	if messages := snapshotMessages(entries); messages[2].Error != "boom" {
		t.Errorf("failed tool message = %+v, want the execution error", messages[2])
	}

	if got := snapshotMessages(nil); len(got) != 0 {
		t.Errorf("snapshotMessages(nil) = %v, want empty", got)
	}
}
