package cmd

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func newTestControl() (*headlessControl, *mocks.FakeAgentService, *services.StateManager) {
	agent := &mocks.FakeAgentService{}
	sm := services.NewStateManager(false)
	return newHeadlessControl(agent, sm, "sess-1"), agent, sm
}

func recvEvent(t *testing.T, ch <-chan domain.ChatEvent) domain.ChatEvent {
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
	if ev, ok := recvEvent(t, ctl.ctrlEvents).(domain.ComputerUsePausedEvent); !ok || ev.RequestID != "sess-1" {
		t.Fatalf("pause event = %+v, want ComputerUsePausedEvent for sess-1", ev)
	}

	ctl.dispatchLine([]byte(`{"type":"computer_use_control","action":"resume"}`))
	ev := recvEvent(t, ctl.ctrlEvents)
	if resumed, ok := ev.(domain.ComputerUseResumedEvent); !ok || resumed.RequestID != "sess-1" {
		t.Fatalf("resume event = %+v, want ComputerUseResumedEvent for sess-1", ev)
	}
	if !ctl.noteControlEvent(ev, false) {
		t.Fatal("resume while paused must mark a pending resume")
	}
	if sm.IsComputerUsePaused() {
		t.Fatal("handling the resume event must clear the paused state")
	}
	if ctl.noteControlEvent(domain.ComputerUseResumedEvent{RequestID: "sess-1"}, false) {
		t.Fatal("resume without a preceding pause must not mark a pending resume")
	}
}

func TestHeadlessControl_PumpPauseResume(t *testing.T) {
	ctl, _, _ := newTestControl()

	first := make(chan domain.ChatEvent, 1)
	first <- domain.ChatChunkEvent{Content: "before pause"}

	resumedRun := make(chan domain.ChatEvent, 1)
	resumedRun <- domain.ChatCompleteEvent{}
	close(resumedRun)

	resumeCalls := 0
	merged := ctl.pumpEvents(first, func() (<-chan domain.ChatEvent, error) {
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
			case domain.ComputerUsePausedEvent:
				paused = true
			case domain.ComputerUseResumedEvent:
				resumed = true
			case domain.ChatCompleteEvent:
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
