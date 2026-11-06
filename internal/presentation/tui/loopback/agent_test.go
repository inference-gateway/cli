package loopback

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

func noJobs() []scheddomain.TrackedJob { return nil }

func startRun(t *testing.T) (chan agentdomain.ChatEvent, <-chan agentdomain.ChatEvent) {
	t.Helper()
	inner := &agentdomainmocks.FakeAgentService{}
	events := make(chan agentdomain.ChatEvent, 8)
	inner.RunWithStreamReturns(events, nil)
	out, err := New(inner, &convmocks.FakeConversationRepository{}, noJobs).RunWithStream(t.Context(), &agentdomain.AgentRequest{RequestID: "req-1", Model: "m"})
	if err != nil {
		t.Fatalf("RunWithStream: %v", err)
	}
	return events, out
}

func next(t *testing.T, out <-chan agentdomain.ChatEvent) agentdomain.ChatEvent {
	t.Helper()
	select {
	case event, ok := <-out:
		if !ok {
			t.Fatal("the stream closed before the expected event")
		}
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")
		return nil
	}
}

func TestAgentStreamsTheRunItReadsBackFromFrames(t *testing.T) {
	events, out := startRun(t)
	events <- agentdomain.ChatChunkEvent{Content: "hi"}
	events <- agentdomain.ChatCompleteEvent{}
	close(events)

	var got []string
	for event := range out {
		if event.GetRequestID() != "req-1" {
			t.Errorf("%T carries request %q, want req-1", event, event.GetRequestID())
		}
		got = append(got, fmt.Sprintf("%T", event))
	}
	want := []string{"domain.ChatStartEvent", "domain.ChatChunkEvent", "domain.ChatCompleteEvent"}
	if !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestAgentAnswersAnApprovalThroughTheRun(t *testing.T) {
	events, out := startRun(t)
	call := sdk.ChatCompletionMessageToolCall{ID: "tc1", Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"rm"}`}}
	asked := agentdomain.ToolApprovalRequestedEvent{ToolCall: call, ResponseChan: make(chan agentdomain.ApprovalAction, 1)}
	events <- agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{call}}
	events <- asked

	var prompt agentdomain.ToolApprovalRequestedEvent
	for {
		if event, ok := next(t, out).(agentdomain.ToolApprovalRequestedEvent); ok {
			prompt = event
			break
		}
	}
	if prompt.ToolCall.ID != "tc1" || prompt.ToolCall.Function.Name != "Bash" {
		t.Fatalf("prompt = %+v, want the Bash call tc1", prompt.ToolCall)
	}
	prompt.ResponseChan <- agentdomain.ApprovalAutoAccept

	select {
	case action := <-asked.ResponseChan:
		if action != agentdomain.ApprovalAutoAccept {
			t.Errorf("the agent got %v, want the auto-accept the user picked", action)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the agent's approval was never answered")
	}
	close(events)
	for range out {
	}
}

func TestAgentLeavesAPlanDecisionToTheChat(t *testing.T) {
	events, out := startRun(t)
	events <- agentdomain.PlanApprovalRequestedEvent{PlanID: "plan-1", PlanContent: "1. do it"}

	var plan agentdomain.PlanApprovalRequestedEvent
	for {
		if event, ok := next(t, out).(agentdomain.PlanApprovalRequestedEvent); ok {
			plan = event
			break
		}
	}
	if plan.PlanID != "plan-1" || plan.PlanContent != "1. do it" {
		t.Errorf("plan = %+v, want the agent's plan", plan)
	}
	if plan.ResponseChan != nil {
		t.Error("the plan carries a response channel, so the chat waits on a worker that does not exist")
	}
	close(events)
	for range out {
	}
}

func TestAgentHandsOverAToolResultBeforeTheNextStep(t *testing.T) {
	events, out := startRun(t)
	events <- agentdomain.ToolExecutionCompletedEvent{
		Results: []*agentdomain.ToolExecutionResult{{ToolName: "Write", ToolCallID: "tc1", Success: true}},
	}

	for {
		if _, ok := next(t, out).(agentdomain.ToolExecutionCompletedEvent); ok {
			break
		}
	}
	close(events)
	for range out {
	}
}

// drain reads the stream until it closes and fails when it stays open.
func drain(t *testing.T, out <-chan agentdomain.ChatEvent) []agentdomain.ChatEvent {
	t.Helper()
	var read []agentdomain.ChatEvent
	for {
		select {
		case event, ok := <-out:
			if !ok {
				return read
			}
			read = append(read, event)
		case <-time.After(5 * time.Second):
			t.Fatal("the stream stayed open")
		}
	}
}

func TestAgentLetsGoOfAPromptTheAgentStoppedWaitingOn(t *testing.T) {
	events, out := startRun(t)
	done := make(chan struct{})
	call := sdk.ChatCompletionMessageToolCall{ID: "tc1", Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"rm"}`}}
	events <- agentdomain.ToolApprovalRequestedEvent{ToolCall: call, ResponseChan: make(chan agentdomain.ApprovalAction, 1), Done: done}

	for {
		if _, ok := next(t, out).(agentdomain.ToolApprovalRequestedEvent); ok {
			break
		}
	}
	close(done)
	close(events)
	drain(t, out)
}

func TestAgentCompletesARunThatHitItsLimit(t *testing.T) {
	tests := []struct {
		name  string
		event agentdomain.ChatCompleteEvent
	}{
		{"time budget", agentdomain.ChatCompleteEvent{MaxDurationReached: true}},
		{"turn limit", agentdomain.ChatCompleteEvent{MaxTurnsReached: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, out := startRun(t)
			events <- tt.event
			close(events)
			for _, event := range drain(t, out) {
				if failed, ok := event.(agentdomain.ChatErrorEvent); ok {
					t.Errorf("the run failed with %v, a local chat completes it", failed.Error)
				}
			}
		})
	}
}

func TestAgentReportsARunThatCannotStart(t *testing.T) {
	inner := &agentdomainmocks.FakeAgentService{}
	inner.RunWithStreamReturns(nil, errors.New("no model"))
	if _, err := New(inner, &convmocks.FakeConversationRepository{}, noJobs).RunWithStream(t.Context(), &agentdomain.AgentRequest{RequestID: "req-1"}); err == nil {
		t.Fatal("RunWithStream returned no error for a run that cannot start")
	}
}
