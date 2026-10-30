package headless

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
)

// encodeRun renders events through a RunEncoder and returns its lines.
func encodeRun(t *testing.T, deps RunEncoderDeps, events ...agentdomain.ChatEvent) []string {
	t.Helper()
	var out strings.Builder
	deps.Model = "m"
	if deps.Repo == nil {
		deps.Repo = &convmocks.FakeConversationRepository{}
	}
	encoder := NewRunEncoder(&out, deps)
	encoder.Start("s1", "run-1")
	for _, event := range events {
		encoder.Handle(event)
	}
	_ = encoder.Finish()
	return strings.Split(strings.TrimSpace(out.String()), "\n")
}

// decodeRun decodes lines and summarizes the events and run ends it saw.
func decodeRun(t *testing.T, lines []string) ([]string, []RunEnd) {
	t.Helper()
	decoder := NewRunDecoder("req-1")
	var summaries []string
	var ends []RunEnd
	for _, line := range lines {
		events, end := decoder.Decode([]byte(line))
		for _, event := range events {
			if event.GetRequestID() != "req-1" {
				t.Errorf("%T carries request %q, want req-1", event, event.GetRequestID())
			}
			summaries = append(summaries, summarize(event))
		}
		if end != RunOpen {
			ends = append(ends, end)
		}
	}
	return summaries, ends
}

// summarize names what a decoded event carries, for comparing scripts.
//
//nolint:gocyclo,cyclop // one case per decoded event type
func summarize(event agentdomain.ChatEvent) string {
	switch e := event.(type) {
	case agentdomain.ChatStartEvent:
		return "start"
	case agentdomain.ChatChunkEvent:
		if e.ReasoningContent != "" {
			return "reasoning:" + e.ReasoningContent
		}
		return "text:" + e.Content
	case agentdomain.ChatCompleteEvent:
		if e.Cancelled {
			return "cancelled"
		}
		calls := make([]string, 0, len(e.ToolCalls))
		for _, call := range e.ToolCalls {
			calls = append(calls, call.ID+"="+call.Function.Name+call.Function.Arguments)
		}
		return fmt.Sprintf("complete:%s|%s|%s", e.Message, e.ReasoningContent, strings.Join(calls, ","))
	case agentdomain.ToolExecutionCompletedEvent:
		ids := make([]string, 0, len(e.Results))
		for _, result := range e.Results {
			ids = append(ids, result.ToolCallID)
		}
		return fmt.Sprintf("results:%s:%d/%d", strings.Join(ids, ","), e.SuccessCount, e.TotalExecuted)
	case agentdomain.ToolApprovalRequestedEvent:
		return "approval:" + e.ToolCall.ID + ":" + e.ToolCall.Function.Name + e.ToolCall.Function.Arguments + ":" + e.Context
	case agentdomain.UserQuestionRequestedEvent:
		return fmt.Sprintf("question:%s:%d", e.ToolCallID, len(e.Questions))
	case agentdomain.PlanApprovalRequestedEvent:
		return "plan:" + e.PlanID + ":" + e.PlanContent
	case agentdomain.ChatErrorEvent:
		return "error:" + e.Error.Error()
	case agentdomain.MessageQueuedEvent:
		text, _ := e.Message.Content.AsMessageContent0()
		return "queued:" + string(e.Message.Role) + ":" + text
	case agentdomain.TodoUpdateChatEvent:
		return fmt.Sprintf("todos:%d", len(e.Todos))
	case agentdomain.JudgeVerdictChatEvent:
		return "judge:" + e.Tool + ":" + string(e.Decision)
	case agentdomain.ToolExecutionProgressEvent:
		return "progress:" + e.ToolCallID + ":" + e.Status
	case agentdomain.BashOutputChunkEvent:
		return fmt.Sprintf("bash:%s:%s:%v", e.ToolCallID, e.Output, e.IsComplete)
	}
	return fmt.Sprintf("%T", event)
}

func bashCall(id, args string) sdk.ChatCompletionMessageToolCall {
	return sdk.ChatCompletionMessageToolCall{ID: id, Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: args}}
}

func TestRunDecoderRoundTripsTheEncoder(t *testing.T) {
	approved := make(chan ipc.ApprovalResponse, 1)
	approved <- ipc.ApprovalResponse{ToolCallID: "tc1", Approved: true}
	answered := make(chan ipc.UserQuestionResponse, 1)
	answered <- ipc.UserQuestionResponse{ToolCallID: "q1", Answers: []byte(`[{"header":"Lang","selectedLabels":["Go"]}]`)}
	rejected := make(chan ipc.PlanApprovalResponse, 1)
	rejected <- ipc.PlanApprovalResponse{PlanID: "plan-1", Action: ipc.PlanReject}
	withUsage := &convmocks.FakeConversationRepository{}
	withUsage.GetSessionTokensReturns(convdomain.SessionTokenStats{RequestCount: 1, TotalInputTokens: 10})

	tests := []struct {
		name   string
		deps   RunEncoderDeps
		events []agentdomain.ChatEvent
		want   []string
		ends   []RunEnd
	}{
		{
			name:   "streamed text",
			events: []agentdomain.ChatEvent{agentdomain.ChatChunkEvent{Content: "hel"}, agentdomain.ChatChunkEvent{Content: "lo"}, agentdomain.ChatCompleteEvent{}},
			want:   []string{"start", "text:hel", "text:lo", "complete:hello||"},
			ends:   []RunEnd{RunFinished},
		},
		{
			name:   "reasoning then text, the step closed by the usage patch",
			deps:   RunEncoderDeps{Repo: withUsage},
			events: []agentdomain.ChatEvent{agentdomain.ChatChunkEvent{ReasoningContent: "think"}, agentdomain.ChatChunkEvent{Content: "answer"}, agentdomain.ChatCompleteEvent{}},
			want:   []string{"start", "reasoning:think", "text:answer", "complete:answer|think|"},
			ends:   []RunEnd{RunFinished},
		},
		{
			name: "tool calls and their results",
			events: []agentdomain.ChatEvent{
				agentdomain.ChatChunkEvent{Content: "run it"},
				agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{bashCall("tc1", `{"command":"ls"}`)}},
				agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolCallID: "tc1", ToolName: "Bash", Success: true}}},
				agentdomain.ChatChunkEvent{Content: "done"},
				agentdomain.ChatCompleteEvent{},
			},
			want: []string{"start", "text:run it", `complete:run it||tc1=Bash{"command":"ls"}`, "results:tc1:1/1", "text:done", "complete:done||"},
			ends: []RunEnd{RunFinished},
		},
		{
			name: "approval suspends and the answer continues",
			deps: RunEncoderDeps{Approvals: approved},
			events: []agentdomain.ChatEvent{
				agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{bashCall("tc1", `{"command":"rm"}`)}},
				agentdomain.ToolApprovalRequestedEvent{ToolCall: bashCall("tc1", `{"command":"rm"}`), Context: "judge escalated", ResponseChan: make(chan agentdomain.ApprovalAction, 1)},
				agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolCallID: "tc1", Success: true}}},
			},
			want: []string{"start", `complete:||tc1=Bash{"command":"rm"}`, `approval:tc1:Bash{"command":"rm"}:judge escalated`, "results:tc1:1/1", "complete:||"},
			ends: []RunEnd{RunSuspended, RunResumed, RunFinished},
		},
		{
			name: "question",
			deps: RunEncoderDeps{Questions: answered},
			events: []agentdomain.ChatEvent{agentdomain.UserQuestionRequestedEvent{
				ToolCallID: "q1", Questions: []agentdomain.UserQuestion{{Header: "Lang", Question: "Which?"}}, ResponseChan: make(chan []agentdomain.UserQuestionAnswer, 1),
			}},
			want: []string{"start", "question:q1:1", "complete:||"},
			ends: []RunEnd{RunSuspended, RunResumed, RunFinished},
		},
		{
			name:   "plan approval",
			deps:   RunEncoderDeps{Plans: rejected},
			events: []agentdomain.ChatEvent{agentdomain.PlanApprovalRequestedEvent{PlanID: "plan-1", PlanContent: "1. do it"}},
			want:   []string{"start", "plan:plan-1:1. do it", "complete:||"},
			ends:   []RunEnd{RunSuspended, RunResumed, RunFinished},
		},
		{
			name:   "cancelled",
			events: []agentdomain.ChatEvent{agentdomain.ChatChunkEvent{Content: "partial"}, agentdomain.ChatCompleteEvent{Cancelled: true}},
			want:   []string{"start", "text:partial", "complete:partial||", "cancelled"},
			ends:   []RunEnd{RunCancelled},
		},
		{
			name:   "error",
			events: []agentdomain.ChatEvent{agentdomain.ChatErrorEvent{Error: errors.New("boom")}},
			want:   []string{"start", "error:boom"},
			ends:   []RunEnd{RunFailed},
		},
		{
			name:   "queued user message",
			events: []agentdomain.ChatEvent{agentdomain.MessageQueuedEvent{RequestID: ipc.UserMessageRequestID, Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("also this")}}},
			want:   []string{"start", "queued:user:also this", "complete:||"},
			ends:   []RunEnd{RunFinished},
		},
		{
			name: "todos, judge, progress and bash output",
			events: []agentdomain.ChatEvent{
				agentdomain.TodoUpdateChatEvent{Todos: []agentdomain.TodoItem{{ID: "1", Content: "do", Status: "pending"}}},
				agentdomain.JudgeVerdictChatEvent{Tool: "Bash", Decision: agentdomain.JudgeDecisionApproved},
				agentdomain.ToolExecutionProgressEvent{ToolCallID: "tc1", ToolName: "Bash", Status: "running"},
				agentdomain.BashOutputChunkEvent{ToolCallID: "tc1", Output: "line"},
			},
			want: []string{"start", "todos:1", "judge:Bash:approved", "progress:tc1:running", "bash:tc1:line:false", "complete:||"},
			ends: []RunEnd{RunFinished},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ends := decodeRun(t, encodeRun(t, tt.deps, tt.events...))
			if !slices.Equal(got, tt.want) {
				t.Errorf("events = %q, want %q", got, tt.want)
			}
			if !slices.Equal(ends, tt.ends) {
				t.Errorf("run ends = %v, want %v", ends, tt.ends)
			}
		})
	}
}

func TestRunDecoderFallsBackToTheStreamedCallForALegacyInterrupt(t *testing.T) {
	lines := []string{
		`{"type":"RUN_STARTED","threadId":"s1","runId":"r1"}`,
		`{"type":"TOOL_CALL_START","toolCallId":"tc9","toolCallName":"Write"}`,
		`{"type":"TOOL_CALL_ARGS","toolCallId":"tc9","delta":"{\"path\":\"a\"}"}`,
		`{"type":"TOOL_CALL_START","toolCallId":"q9","toolCallName":"AskUserQuestion"}`,
		`{"type":"TOOL_CALL_ARGS","toolCallId":"q9","delta":"{\"questions\":[{\"header\":\"H\",\"question\":\"Q\"}]}"}`,
		`{"type":"RUN_FINISHED","threadId":"s1","runId":"r1","outcome":{"type":"interrupt","interrupts":[{"id":"tc9","reason":"tool_call","toolCallId":"tc9"},{"id":"q9","reason":"input_required"},{"id":"x","reason":"mystery"}]}}`,
	}
	decoder := NewRunDecoder("req-1")
	var got []string
	var end RunEnd
	for _, line := range lines {
		var events []agentdomain.ChatEvent
		events, end = decoder.Decode([]byte(line))
		for _, event := range events {
			got = append(got, summarize(event))
		}
	}
	want := []string{"start", `complete:||tc9=Write{"path":"a"},q9=AskUserQuestion{"questions":[{"header":"H","question":"Q"}]}`, `approval:tc9:Write{"path":"a"}:`, "question:q9:1"}
	if !slices.Equal(got, want) || end != RunSuspended {
		t.Fatalf("events = %q ending %v, want %q suspended", got, end, want)
	}
	prompts := decoder.Prompts()
	if len(prompts) != 3 || prompts[2].InterruptID != "x" || prompts[2].Event != nil {
		t.Fatalf("prompts = %+v, want three with the unknown one carrying no event", prompts)
	}
}

func TestRunDecoderToleratesAttachingMidRun(t *testing.T) {
	lines := []string{
		`{"type":"TEXT_MESSAGE_CONTENT","messageId":"m1","delta":"…rest"}`,
		`{"type":"TEXT_MESSAGE_END","messageId":"m1"}`,
		`{"type":"RUN_FINISHED","threadId":"s1","runId":"r1","outcome":{"type":"success"}}`,
	}
	got, ends := decodeRun(t, lines)
	if want := []string{"text:…rest", "complete:…rest||"}; !slices.Equal(got, want) || !slices.Equal(ends, []RunEnd{RunFinished}) {
		t.Fatalf("events = %q ends %v, want %q finished", got, ends, want)
	}
}

func TestRunDecoderEndsADirectCallRunWithACompletion(t *testing.T) {
	lines := encodeRun(t, RunEncoderDeps{},
		agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{bashCall("tc1", `{"command":"ls"}`)}},
		agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolCallID: "tc1", ToolName: "Bash", Success: true}}, SuccessCount: 1, TotalExecuted: 1},
	)

	events, ends := decodeRun(t, lines)

	want := []string{"start", `complete:||tc1=Bash{"command":"ls"}`, "results:tc1:1/1", "complete:||"}
	if !slices.Equal(events, want) || !slices.Equal(ends, []RunEnd{RunFinished}) {
		t.Errorf("events = %q ending %v, want %q ending the turn", events, ends, want)
	}
}

func TestRunDecoderEndsARunWithoutStepsWithACompletion(t *testing.T) {
	events, ends := decodeRun(t, encodeRun(t, RunEncoderDeps{}))

	if want := []string{"start", "complete:||"}; !slices.Equal(events, want) || !slices.Equal(ends, []RunEnd{RunFinished}) {
		t.Errorf("events = %q ending %v, want %q so the turn ends", events, ends, want)
	}
}

func TestRunDecoderEndsTheTurnOnlyWithTheRun(t *testing.T) {
	withUsage := &convmocks.FakeConversationRepository{}
	withUsage.GetSessionTokensReturns(convdomain.SessionTokenStats{RequestCount: 1, TotalInputTokens: 10})
	lines := encodeRun(t, RunEncoderDeps{Repo: withUsage},
		agentdomain.ChatChunkEvent{Content: "first"}, agentdomain.ChatCompleteEvent{},
		agentdomain.MessageQueuedEvent{RequestID: ipc.UserMessageRequestID, Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("more")}},
		agentdomain.ChatChunkEvent{Content: "second"}, agentdomain.ChatCompleteEvent{},
	)
	decoder := NewRunDecoder("req-1")
	var events []string

	for _, line := range lines {
		decoded, end := decoder.Decode([]byte(line))
		for _, event := range decoded {
			if complete, ok := event.(agentdomain.ChatCompleteEvent); ok && complete.Message == "second" && end != RunFinished {
				t.Error("the last step ended the turn before its run finished")
			}
			events = append(events, summarize(event))
		}
	}

	want := []string{"start", "text:first", "complete:first||", "queued:user:more", "text:second", "complete:second||"}
	if !slices.Equal(events, want) {
		t.Errorf("events = %q, want %q", events, want)
	}
}
