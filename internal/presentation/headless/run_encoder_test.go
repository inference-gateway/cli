package headless

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
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
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

// wireEvent is the slice of an AG-UI event the headless runtime tests assert
// on: the lifecycle fields, and the payload fields a render-level test reads.
type wireEvent struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Delta   json.RawMessage `json:"delta"`
	Outcome struct {
		Type       string          `json:"type"`
		Interrupts []wireInterrupt `json:"interrupts"`
	} `json:"outcome"`
	Usage  []map[string]any `json:"usage"`
	Result map[string]any   `json:"result"`
}

// wireInterrupt is the slice of an interrupt the tests assert on.
type wireInterrupt struct {
	ID             string `json:"id"`
	Reason         string `json:"reason"`
	ToolCallID     string `json:"toolCallId"`
	ResponseSchema any    `json:"responseSchema"`
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

func eventTypes(events []wireEvent) []string {
	types := make([]string, 0, len(events))
	for _, ev := range events {
		types = append(types, ev.Type)
	}
	return types
}

// interruptAt returns the index of the RUN_FINISHED that suspended the run on
// the interrupt outcome, -1 when it never suspended.
func interruptAt(events []wireEvent) int {
	return slices.IndexFunc(events, func(ev wireEvent) bool {
		return ev.Type == "RUN_FINISHED" && ev.Outcome.Type == "interrupt"
	})
}

// continuationAt returns the index of the RUN_STARTED that opens the next run
// at or after from, -1 when none follows.
func continuationAt(events []wireEvent, from int) int {
	return from + slices.IndexFunc(events[from:], func(ev wireEvent) bool {
		return ev.Type == "RUN_STARTED"
	})
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
			name:     "failure after a cancelled turn is a run error",
			events:   []agentdomain.ChatEvent{agentdomain.ChatCompleteEvent{Cancelled: true}, agentdomain.ChatErrorEvent{Error: boom}},
			wantType: "RUN_ERROR",
			wantErr:  boom,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			r := NewRunEncoder(&out, RunEncoderDeps{Model: "m", Repo: &convmocks.FakeConversationRepository{}})
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
		{"resumed run snapshots right after RUN_STARTED", history, []string{"RUN_STARTED", "STATE_SNAPSHOT", "MESSAGES_SNAPSHOT"}},
		{"fresh run emits no snapshot", nil, []string{"RUN_STARTED", "STATE_SNAPSHOT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			NewRunEncoder(&out, RunEncoderDeps{Model: "m", Repo: &convmocks.FakeConversationRepository{}, History: tt.history}).Start("s1", "run-1")
			if got := eventTypes(decodeEvents(t, out.String())); !slices.Equal(got, tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunEncoder_EmitsOneWritePerEvent(t *testing.T) {
	w := &writeRecordingWriter{}
	r := NewRunEncoder(w, RunEncoderDeps{Model: "m"})
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
	want := []string{"RUN_STARTED", "STATE_SNAPSHOT", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if !slices.Equal(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
}

func TestRunEncoder_ApprovalSuspendsTheRunAndTheAnswerContinues(t *testing.T) {
	respChan := make(chan agentdomain.ApprovalAction, 1)
	approvals := approvalsChan(ipc.ApprovalResponse{ToolCallID: "tc1", Approved: true})
	var out strings.Builder
	r := NewRunEncoder(&out, RunEncoderDeps{Model: "m", Repo: &convmocks.FakeConversationRepository{}, Approvals: approvals})
	r.Start("s1", "run-1")
	r.Handle(agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{
		{ID: "tc1", Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{}`}},
	}})
	r.Handle(agentdomain.ToolApprovalRequestedEvent{
		ToolCall:     sdk.ChatCompletionMessageToolCall{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"}},
		ResponseChan: respChan,
	})
	select {
	case got := <-respChan:
		if got != agentdomain.ApprovalApprove {
			t.Fatalf("approval action = %v, want ApprovalApprove", got)
		}
	default:
		t.Fatal("no approval action sent on ResponseChan")
	}

	r.Handle(agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{
		ToolCallID: "tc1", Success: true,
	}}})
	if err := r.Finish(); err != nil {
		t.Fatalf("Finish() err = %v", err)
	}

	events := decodeEvents(t, out.String())
	suspended := interruptAt(events)
	if suspended < 0 {
		t.Fatalf("no interrupt outcome on the wire:\n%s", out.String())
	}
	interrupts := events[suspended].Outcome.Interrupts
	if len(interrupts) != 1 || interrupts[0].ToolCallID != "tc1" || interrupts[0].Reason != agui.InterruptToolCall {
		t.Errorf("interrupts = %+v, want one tool_call interrupt for tc1", interrupts)
	}
	continuation := continuationAt(events, suspended+1)
	if continuation < 0 {
		t.Fatalf("the answered interrupt did not continue the run:\n%s", out.String())
	}
	types := eventTypes(events[continuation:])
	if !slices.Contains(types, "TOOL_CALL_RESULT") {
		t.Errorf("the continuation run lacks the answered call's TOOL_CALL_RESULT: %v", types)
	}
}

func TestRunEncoder_QuestionSuspendsTheRunAndTheAnswerContinues(t *testing.T) {
	answers := json.RawMessage(`[{"header":"Lang","question":"Which?","selectedLabels":["Go"]}]`)
	questions := questionsChan(ipc.UserQuestionResponse{ToolCallID: "q1", Answers: answers})
	respChan := make(chan []agentdomain.UserQuestionAnswer, 1)
	var out strings.Builder
	r := NewRunEncoder(&out, RunEncoderDeps{Model: "m", Repo: &convmocks.FakeConversationRepository{}, Questions: questions})
	r.Start("s1", "run-1")
	r.Handle(agentdomain.UserQuestionRequestedEvent{
		ToolCallID:   "q1",
		Questions:    []agentdomain.UserQuestion{{Header: "Lang", Question: "Which?", Options: []agentdomain.UserQuestionOption{{Label: "Go"}, {Label: "Rust"}}}},
		ResponseChan: respChan,
	})
	received, open := <-respChan
	if !open || len(received) != 1 || strings.Join(received[0].SelectedLabels, ",") != "Go" {
		t.Fatalf("answers = %+v (open=%v), want the Go label", received, open)
	}
	if err := r.Finish(); err != nil {
		t.Fatalf("Finish() err = %v", err)
	}
	events := decodeEvents(t, out.String())
	suspended := interruptAt(events)
	if suspended < 0 {
		t.Fatalf("no interrupt outcome on the wire:\n%s", out.String())
	}
	interrupts := events[suspended].Outcome.Interrupts
	if len(interrupts) != 1 || interrupts[0].Reason != agui.InterruptInputRequired || interrupts[0].ResponseSchema == nil {
		t.Fatalf("interrupts = %+v, want one input_required interrupt with a responseSchema", interrupts)
	}
	if continuation := continuationAt(events, suspended+1); continuation < 0 {
		t.Fatalf("the answered question did not continue the run:\n%s", out.String())
	}
}
