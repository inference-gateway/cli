package headless

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
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
	Usage        []map[string]any `json:"usage"`
	Result       map[string]any   `json:"result"`
	MessageID    string           `json:"messageId"`
	ActivityType string           `json:"activityType"`
	Content      json.RawMessage  `json:"content"`
}

// wireInterrupt is the slice of an interrupt the tests assert on.
type wireInterrupt struct {
	ID             string         `json:"id"`
	Reason         string         `json:"reason"`
	ToolCallID     string         `json:"toolCallId"`
	Message        string         `json:"message"`
	ResponseSchema any            `json:"responseSchema"`
	Metadata       map[string]any `json:"metadata"`
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

func TestRunEncoder_SubmittedJobPatchesBackgroundTasks(t *testing.T) {
	w := &writeRecordingWriter{}
	jobs := func() []scheddomain.TrackedJob {
		return []scheddomain.TrackedJob{{
			Meta:   scheddomain.JobMeta{ID: "j1", Kind: scheddomain.JobKindSubagent, Label: "write docs"},
			Status: scheddomain.JobRunning,
		}}
	}
	r := NewRunEncoder(w, RunEncoderDeps{Model: "m", Jobs: jobs})
	r.Start("s1", "run-1")
	r.Handle(agentdomain.BackgroundTasksChangedEvent{})
	last := decodeEvents(t, w.writes[len(w.writes)-1])[0]
	if last.Type != "STATE_DELTA" {
		t.Fatalf("last event = %s, want a STATE_DELTA patching /backgroundTasks", last.Type)
	}
	patch := patches(t, last)
	if len(patch) != 1 || patch[0].Path != "/backgroundTasks" {
		t.Fatalf("patches = %#v, want one on /backgroundTasks", patch)
	}
	if tasks, _ := patch[0].Value.(map[string]any); tasks["running"] != float64(1) {
		t.Errorf("running = %v, want 1", patch[0].Value)
	}
}

func TestRunEncoder_PublishesToolActivity(t *testing.T) {
	tests := []struct {
		name         string
		event        agentdomain.ChatEvent
		messageID    string
		activityType string
		want         map[string]any
	}{
		{
			name:         "tool progress",
			event:        agentdomain.ToolExecutionProgressEvent{ToolCallID: "tc1", ToolName: "Bash", Status: "running", Message: "Executing..."},
			messageID:    "tool:tc1",
			activityType: "tool_progress",
			want:         map[string]any{"toolCallId": "tc1", "toolName": "Bash", "status": "running", "message": "Executing..."},
		},
		{
			name:         "bash output chunk",
			event:        agentdomain.BashOutputChunkEvent{ToolCallID: "tc1", Output: "line 1\n", IsComplete: true},
			messageID:    "bash:tc1",
			activityType: "bash_output",
			want:         map[string]any{"toolCallId": "tc1", "output": "line 1\n", "complete": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writeRecordingWriter{}
			r := NewRunEncoder(w, RunEncoderDeps{Model: "m"})
			r.Start("s1", "run-1")
			r.Handle(tt.event)
			last := decodeEvents(t, w.writes[len(w.writes)-1])[0]
			var content map[string]any
			if err := json.Unmarshal(last.Content, &content); err != nil {
				t.Fatalf("activity content %s: %v", last.Content, err)
			}
			if last.Type != "ACTIVITY_SNAPSHOT" || last.MessageID != tt.messageID || last.ActivityType != tt.activityType || !maps.Equal(content, tt.want) {
				t.Errorf("event = %s %s %s %v, want ACTIVITY_SNAPSHOT %s %s %v", last.Type, last.MessageID, last.ActivityType, content, tt.messageID, tt.activityType, tt.want)
			}
		})
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
		ToolCall:     sdk.ChatCompletionMessageToolCall{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"ls"}`}},
		Context:      "the judge escalated it",
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
		t.Fatalf("interrupts = %+v, want one tool_call interrupt for tc1", interrupts)
	}
	if got := interrupts[0]; got.Message != "the judge escalated it" || got.Metadata["toolName"] != "Bash" || got.Metadata["arguments"] != `{"command":"ls"}` {
		t.Errorf("interrupt = %+v, want the context as message and the call in its metadata", got)
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

func TestRunEncoder_PlanApprovalSuspendsAndAppliesTheDecision(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		wantMode   agentdomain.AgentMode
		wantStatus convdomain.PlanApprovalStatus
		execute    bool
	}{
		{name: "accept runs in auto-accept", action: ipc.PlanAccept, wantMode: agentdomain.AgentModeAutoAccept, wantStatus: convdomain.PlanApprovalAccepted, execute: true},
		{name: "accept standard approves each step", action: ipc.PlanAcceptStandard, wantMode: agentdomain.AgentModeStandard, wantStatus: convdomain.PlanApprovalAccepted, execute: true},
		{name: "reject keeps planning", action: ipc.PlanReject, wantStatus: convdomain.PlanApprovalRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plans := make(chan ipc.PlanApprovalResponse, 1)
			plans <- ipc.PlanApprovalResponse{PlanID: "plan-1", Action: tt.action}
			modes := &agentdomainmocks.FakeAgentModeState{}
			repo := conversation.NewInMemoryConversationRepository(nil, nil)
			if err := repo.AddMessage(convdomain.ConversationEntry{IsPlan: true, Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("1. do it")}}); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			r := NewRunEncoder(&out, RunEncoderDeps{Model: "m", Repo: repo, Plans: plans, Modes: modes})
			r.Start("s1", "run-1")
			r.Handle(agentdomain.PlanApprovalRequestedEvent{PlanID: "plan-1", PlanContent: "1. do it"})
			r.Handle(agentdomain.ChatCompleteEvent{})
			if err := r.Finish(); err != nil {
				t.Fatalf("Finish() err = %v", err)
			}

			events := decodeEvents(t, out.String())
			suspended := interruptAt(events)
			if suspended < 0 {
				t.Fatalf("no interrupt outcome on the wire:\n%s", out.String())
			}
			interrupt := events[suspended].Outcome.Interrupts[0]
			if interrupt.ID != "plan-1" || interrupt.Reason != interruptPlanApproval || interrupt.Metadata["plan"] != "1. do it" || interrupt.ResponseSchema == nil {
				t.Fatalf("interrupt = %+v, want the plan_approval interrupt carrying the plan", interrupt)
			}
			if continuationAt(events, suspended+1) < 0 {
				t.Fatalf("the decided plan did not continue the run:\n%s", out.String())
			}
			if planID, execute := r.AcceptedPlan(); execute != tt.execute || (execute && planID != "plan-1") {
				t.Errorf("AcceptedPlan() = %q, %v, want execute %v", planID, execute, tt.execute)
			}
			if got := repo.GetMessages()[0].PlanApprovalStatus; got != tt.wantStatus {
				t.Errorf("plan status = %v, want %v", got, tt.wantStatus)
			}
			switch {
			case tt.execute && (modes.SetAgentModeCallCount() != 1 || modes.SetAgentModeArgsForCall(0) != tt.wantMode):
				t.Errorf("agent mode set %d times, want once to %v", modes.SetAgentModeCallCount(), tt.wantMode)
			case !tt.execute && modes.SetAgentModeCallCount() != 0:
				t.Errorf("a rejected plan changed the agent mode")
			}
		})
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
	if asked, _ := interrupts[0].Metadata["questions"].([]any); len(asked) != 1 {
		t.Errorf("interrupt metadata = %+v, want the one question it asks", interrupts[0].Metadata)
	}
	if continuation := continuationAt(events, suspended+1); continuation < 0 {
		t.Fatalf("the answered question did not continue the run:\n%s", out.String())
	}
}
