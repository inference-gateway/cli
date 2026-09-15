package headless

import (
	"strings"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	conversationmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

func newTestControl() (*headlessControl, *agentdomainmocks.FakeAgentService, *statemanager.StateManager) {
	agent := &agentdomainmocks.FakeAgentService{}
	sm := statemanager.NewStateManager(false)
	return newHeadlessControl(agent, sm, &conversationmocks.FakeMessageQueue{}, "sess-1"), agent, sm
}

func recvEvent(t *testing.T, ch <-chan agentdomain.ChatEvent) agentdomain.ChatEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

func TestHeadlessControl_DispatchLine(t *testing.T) {
	ctl, agent, sm := newTestControl()

	ctl.dispatchLine([]byte(`{"type":"approval_response","tool_call_id":"tc1","approved":true}`))
	select {
	case resp := <-ctl.approvals:
		if resp.ToolCallID != "tc1" || !resp.Approved {
			t.Fatalf("approval response = %+v, want tc1 approved", resp)
		}
	default:
		t.Fatal("approval_response line not forwarded to approvals channel")
	}

	ctl.dispatchLine([]byte(`{"type":"user_question_response","tool_call_id":"tc2","answers":[{"header":"H","question":"Q","selectedLabels":["A"]}]}`))
	select {
	case resp := <-ctl.questions:
		if resp.ToolCallID != "tc2" || resp.Cancelled || !strings.Contains(string(resp.Answers), `"A"`) {
			t.Fatalf("question response = %+v, want tc2 answered", resp)
		}
	default:
		t.Fatal("user_question_response line not forwarded to questions channel")
	}

	for _, noise := range []string{"not json", `{"type":"other"}`, `{"type":"computer_use_control","action":"nonsense"}`} {
		ctl.dispatchLine([]byte(noise))
	}
	select {
	case ev := <-ctl.ctrlEvents:
		t.Fatalf("noise line produced control event %+v", ev)
	default:
	}
	if agent.CancelRequestCallCount() != 0 {
		t.Fatal("noise line cancelled the request")
	}

	ctl.dispatchLine([]byte(`{"type":"computer_use_control","action":"pause"}`))
	if agent.CancelRequestCallCount() != 1 || agent.CancelRequestArgsForCall(0) != "sess-1" {
		t.Fatalf("pause must cancel the session request, got %d calls", agent.CancelRequestCallCount())
	}
	if !sm.IsComputerUsePaused() {
		t.Fatal("pause must set the paused state")
	}
	if ev, ok := recvEvent(t, ctl.ctrlEvents).(agentdomain.ComputerUsePausedEvent); !ok || ev.RequestID != "sess-1" {
		t.Fatalf("pause event = %+v, want ComputerUsePausedEvent for sess-1", ev)
	}

	ctl.dispatchLine([]byte(`{"type":"computer_use_control","action":"resume"}`))
	ev := recvEvent(t, ctl.ctrlEvents)
	if resumed, ok := ev.(agentdomain.ComputerUseResumedEvent); !ok || resumed.RequestID != "sess-1" {
		t.Fatalf("resume event = %+v, want ComputerUseResumedEvent for sess-1", ev)
	}
	if !ctl.noteControlEvent(ev, false) {
		t.Fatal("resume while paused must mark a pending resume")
	}
	if sm.IsComputerUsePaused() {
		t.Fatal("handling the resume event must clear the paused state")
	}
	if ctl.noteControlEvent(agentdomain.ComputerUseResumedEvent{RequestID: "sess-1"}, false) {
		t.Fatal("resume without a preceding pause must not mark a pending resume")
	}
}

func TestHeadlessControl_PumpPauseResume(t *testing.T) {
	ctl, _, _ := newTestControl()

	first := make(chan agentdomain.ChatEvent, 1)
	first <- agentdomain.ChatChunkEvent{Content: "before pause"}

	resumedRun := make(chan agentdomain.ChatEvent, 1)
	resumedRun <- agentdomain.ChatCompleteEvent{}
	close(resumedRun)

	resumeCalls := 0
	merged := ctl.pumpEvents(first, func() (<-chan agentdomain.ChatEvent, error) {
		resumeCalls++
		return resumedRun, nil
	})

	ctl.dispatchLine([]byte(`{"type":"computer_use_control","action":"pause"}`))
	close(first)
	ctl.dispatchLine([]byte(`{"type":"computer_use_control","action":"resume"}`))

	var paused, resumed, completed bool
	deadline := time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-merged:
			if !ok {
				done = true
				continue
			}
			switch ev.(type) {
			case agentdomain.ComputerUsePausedEvent:
				paused = true
			case agentdomain.ComputerUseResumedEvent:
				resumed = true
			case agentdomain.ChatCompleteEvent:
				completed = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for merged channel to close")
		}
	}
	if !paused || !resumed || !completed {
		t.Fatalf("merged events missing: paused=%v resumed=%v completed=%v", paused, resumed, completed)
	}
	if resumeCalls != 1 {
		t.Fatalf("resume() called %d times, want 1", resumeCalls)
	}
}

func TestHeadlessControl_ReadLinesSurvivesLargeLine(t *testing.T) {
	ctl, _, _ := newTestControl()
	large := `{"type":"approval_response","tool_call_id":"` + strings.Repeat("x", 128*1024) + `","approved":false}`
	go ctl.readLines(strings.NewReader(large + "\n" + `{"type":"approval_response","tool_call_id":"tc2","approved":true}` + "\n"))

	for {
		select {
		case resp, ok := <-ctl.approvals:
			if !ok {
				t.Fatal("stdin reader gave up before the line following a >64KiB line")
			}
			if resp.ToolCallID == "tc2" {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for the approval after a large line")
		}
	}
}

func TestHeadlessControl_UserMessage(t *testing.T) {
	ctl, _, _ := newTestControl()
	queue := ctl.messageQueue.(*conversationmocks.FakeMessageQueue)

	ctl.dispatchLine([]byte(`{"type":"user_message","content":"finally open it"}`))
	if queue.EnqueueCallCount() != 1 {
		t.Fatalf("user_message enqueue calls = %d, want 1", queue.EnqueueCallCount())
	}
	if msg, reqID := queue.EnqueueArgsForCall(0); reqID != ipc.UserMessageRequestID || msg.Role != sdk.User {
		t.Fatalf("enqueued (%+v, %q), want user role tagged %q", msg, reqID, ipc.UserMessageRequestID)
	}

	ctl.dispatchLine([]byte(`{"type":"user_message","content":""}`))
	if queue.EnqueueCallCount() != 1 {
		t.Fatal("empty user_message landed on the message queue")
	}
}
