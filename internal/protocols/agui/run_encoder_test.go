package agui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
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

func TestRunEncoder_CancelledRunFinishesWithCancelledOutcome(t *testing.T) {
	var out strings.Builder
	r := NewRunEncoder(&out, "m", &convmocks.FakeConversationRepository{}, nil, nil, nil, nil)
	r.Start("s1", "run-1")
	r.Handle(agentdomain.ChatChunkEvent{Content: "partial"})
	r.Handle(agentdomain.ChatCompleteEvent{Cancelled: true})
	err := r.Finish()
	if err == nil {
		t.Fatal("Finish() err = nil, want the cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Finish() err = %v, want context.Canceled", err)
	}
	got := out.String()
	if strings.Contains(got, `"RUN_ERROR"`) {
		t.Errorf("cancelled run must not emit RUN_ERROR\n%s", got)
	}
	if !strings.Contains(got, `"RUN_FINISHED"`) {
		t.Fatalf("cancelled run must still emit RUN_FINISHED\n%s", got)
	}
	for line := range strings.SplitSeq(got, "\n") {
		if !strings.Contains(line, `"RUN_FINISHED"`) {
			continue
		}
		var finished struct {
			Outcome struct {
				Type string `json:"type"`
			} `json:"outcome"`
		}
		if err := json.Unmarshal([]byte(line), &finished); err != nil {
			t.Fatalf("RUN_FINISHED line is not valid JSON: %v\n%s", err, line)
		}
		if finished.Outcome.Type != "cancelled" {
			t.Errorf("RUN_FINISHED outcome.type = %q, want cancelled\n%s", finished.Outcome.Type, got)
		}
		break
	}
}

func TestRunEncoder_ComputerUseResumedClearsCancelledOutcome(t *testing.T) {
	var out strings.Builder
	r := NewRunEncoder(&out, "m", &convmocks.FakeConversationRepository{}, nil, nil, nil, nil)
	r.Start("s1", "run-1")
	r.Handle(agentdomain.ChatCompleteEvent{Cancelled: true})
	r.Handle(agentdomain.ComputerUseResumedEvent{RequestID: "s1"})
	err := r.Finish()
	if err != nil {
		t.Fatalf("Finish() err = %v, want nil after resume clears the cancellation", err)
	}
	got := out.String()
	if strings.Contains(got, `"RUN_ERROR"`) {
		t.Errorf("resumed run must not emit RUN_ERROR\n%s", got)
	}
	if lines := strings.Count(got, `"RUN_FINISHED"`); lines != 1 {
		t.Errorf("RUN_FINISHED count = %d, want 1\n%s", lines, got)
	}
}

func TestRunEncoder_StartEmitsMessagesSnapshotWhenResuming(t *testing.T) {
	toolCalls := []sdk.ChatCompletionMessageToolCall{
		{ID: "tc1", Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"ls"}`}},
	}
	toolCallID := "tc1"
	history := []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("run the checks")}},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("working on it"), ToolCalls: &toolCalls}},
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("boom"), ToolCallID: &toolCallID},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolName: "Bash", ToolCallID: "tc1", Success: false, Error: "boom happened"},
		},
	}

	var out strings.Builder
	r := NewRunEncoder(&out, "m", &convmocks.FakeConversationRepository{}, history, nil, nil, nil)
	r.Start("s1", "run-1")

	got := out.String()
	started := strings.Index(got, `"RUN_STARTED"`)
	snapshot := strings.Index(got, `"MESSAGES_SNAPSHOT"`)
	if started < 0 || snapshot < 0 || snapshot < started {
		t.Fatalf("MESSAGES_SNAPSHOT must come right after RUN_STARTED\n%s", got)
	}

	var snap struct {
		Type     string          `json:"type"`
		Messages []snapshotEntry `json:"messages"`
	}
	for line := range strings.SplitSeq(got, "\n") {
		if !strings.Contains(line, `"MESSAGES_SNAPSHOT"`) {
			continue
		}
		if err := json.Unmarshal([]byte(line), &snap); err != nil {
			t.Fatalf("MESSAGES_SNAPSHOT line is not valid JSON: %v\n%s", err, line)
		}
		break
	}
	if snap.Type != "MESSAGES_SNAPSHOT" {
		t.Fatalf("snapshot type = %q, want MESSAGES_SNAPSHOT\n%s", snap.Type, got)
	}
	if len(snap.Messages) != 3 {
		t.Fatalf("snapshot messages = %d, want 3\n%s", len(snap.Messages), got)
	}
	for i, msg := range snap.Messages {
		if msg.ID == "" {
			t.Errorf("snapshot message %d has no id\n%s", i, got)
		}
	}
	if snap.Messages[0].Role != "user" || snap.Messages[0].Content != "run the checks" {
		t.Errorf("snapshot user message = %+v, want role user with its text\n%s", snap.Messages[0], got)
	}
	assistant := snap.Messages[1]
	if assistant.Role != "assistant" || assistant.Content != "working on it" {
		t.Errorf("snapshot assistant message = %+v, want role assistant with its text\n%s", assistant, got)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "tc1" || assistant.ToolCalls[0].Function.Name != "Bash" {
		t.Errorf("snapshot assistant toolCalls = %+v, want the stored Bash call\n%s", assistant.ToolCalls, got)
	}
	tool := snap.Messages[2]
	if tool.Role != "tool" || tool.ToolCallID != "tc1" || tool.Error != "boom happened" {
		t.Errorf("snapshot tool message = %+v, want toolCallId tc1 and the execution error\n%s", tool, got)
	}
}

func TestRunEncoder_FreshRunEmitsNoMessagesSnapshot(t *testing.T) {
	var out strings.Builder
	r := NewRunEncoder(&out, "m", &convmocks.FakeConversationRepository{}, nil, nil, nil, nil)
	r.Start("s1", "run-1")
	if err := r.Finish(); err != nil {
		t.Fatalf("Finish() err = %v", err)
	}
	if strings.Contains(out.String(), `"MESSAGES_SNAPSHOT"`) {
		t.Errorf("fresh run must not emit MESSAGES_SNAPSHOT\n%s", out.String())
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
	// RUN_STARTED, TEXT_MESSAGE_START, TEXT_MESSAGE_CONTENT, TEXT_MESSAGE_END,
	// RUN_FINISHED - each a single Write of one event.
	if len(w.writes) != 5 {
		t.Fatalf("Write call count = %d, want 5\n%v", len(w.writes), w.writes)
	}
	for i, write := range w.writes {
		if !strings.HasSuffix(write, "\n") {
			t.Errorf("write %d is not newline-terminated: %q", i, write)
		}
		var ev struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSuffix(write, "\n")), &ev); err != nil || ev.Type == "" {
			t.Errorf("write %d does not carry exactly one event: %q (err=%v)", i, write, err)
		}
	}
}

// snapshotEntry mirrors the AG-UI message fields the snapshot maps.
type snapshotEntry struct {
	ID         string         `json:"id"`
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []snapshotCall `json:"toolCalls,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type snapshotCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
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
	}
	messages := snapshotMessages(entries)
	if len(messages) != 3 {
		t.Fatalf("snapshotMessages() = %d messages, want 3", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hi" || messages[0].ID == "" {
		t.Errorf("user message = %+v, want role user with content and an id", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].ToolCallID != "" {
		t.Errorf("assistant message = %+v, want role assistant without toolCallId", messages[1])
	}
	if tool := messages[2]; tool.ToolCallID != "tc1" || tool.Error != "" {
		t.Errorf("successful tool message = %+v, want toolCallId tc1 without error", tool)
	}

	failed := entries
	failed[2].ToolExecution = &agentdomain.ToolExecutionResult{ToolCallID: "tc1", Success: false, Error: "boom"}
	if messages := snapshotMessages(failed); messages[2].Error != "boom" {
		t.Errorf("failed tool message = %+v, want the execution error", messages[2])
	}

	if got := snapshotMessages(nil); len(got) != 0 {
		t.Errorf("snapshotMessages(nil) = %v, want empty", got)
	}
}

func TestRunEncoder_BackgroundJobsStillStreamedPerRun(t *testing.T) {
	jobs := func() []scheddomain.TrackedJob {
		return []scheddomain.TrackedJob{
			{Meta: scheddomain.JobMeta{ID: "task-1", Kind: scheddomain.JobKindA2A, Label: "delay"}, Status: scheddomain.JobRunning},
		}
	}
	var out strings.Builder
	r := NewRunEncoder(&out, "m", &convmocks.FakeConversationRepository{}, nil, jobs, nil, nil)
	r.Start("s1", "run-1")
	r.Handle(agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolName: "Read", ToolCallID: "c1", Success: true}}})
	if err := r.Finish(); err != nil {
		t.Fatalf("Finish() err = %v", err)
	}
	if n := strings.Count(out.String(), `"name":"background_tasks"`); n != 1 {
		t.Errorf("background_tasks count = %d, want 1\n%s", n, out.String())
	}
}

func TestRunEncoder_EmitRunErrorStandalone(t *testing.T) {
	var out strings.Builder
	r := &RunEncoder{w: &out}
	r.emitRunError("gateway down")
	for _, want := range []string{`"RUN_ERROR"`, "gateway down"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %s in %s", want, out.String())
		}
	}
}
