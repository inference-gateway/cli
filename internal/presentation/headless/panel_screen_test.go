package headless

import (
	"errors"
	"strings"
	"testing"

	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	computermocks "github.com/inference-gateway/cli/tests/mocks/computer"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
)

func errorText(frame map[string]any) string {
	text, _ := frame["error"].(string)
	return text
}

func screenDeps(control *computermocks.FakeScreenControl, queue *convmocks.FakeMessageQueue) PanelDeps {
	deps := panelDeps()
	deps.Screen = control
	deps.Queue = queue
	return deps
}

func TestPanelScreenTakeOverClaimsAndResolvesTheOpenApproval(t *testing.T) {
	control := &computermocks.FakeScreenControl{}
	panel, sink := startPanel(screenDeps(control, nil))
	resolved := 0
	panel.screen.resolve = func() { resolved++ }

	if !handle(t, panel, map[string]any{"type": "screen_control", "id": "sreq", "action": "take_over"}) {
		t.Fatal("the screen_control frame was left to the caller")
	}

	frame := readFrame(t, sink, "screen_result")
	if frame["id"] != "sreq" || frame["held"] != true {
		t.Fatalf("screen_result = %v, want id sreq held", frame)
	}
	if control.TakeOverCallCount() != 1 {
		t.Fatalf("the person claimed the screen %d times, want once", control.TakeOverCallCount())
	}
	if resolved != 1 {
		t.Fatalf("the take-over resolved %d open approvals, want one", resolved)
	}
}

func TestPanelScreenTakeOverRefusedWhenSomeoneElseHolds(t *testing.T) {
	control := &computermocks.FakeScreenControl{}
	control.TakeOverReturns(errors.New("the screen is already held by the person"))
	control.HeldReturns(true)
	panel, sink := startPanel(screenDeps(control, nil))

	if !handle(t, panel, map[string]any{"type": "screen_control", "id": "sreq", "action": "take_over"}) {
		t.Fatal("the screen_control frame was left to the caller")
	}

	frame := readFrame(t, sink, "screen_result")
	if frame["held"] != true || !strings.Contains(errorText(frame), "already held") {
		t.Fatalf("screen_result = %v, want the held screen with the refusal", frame)
	}
}

func TestPanelScreenHandBackQueuesTheContinueMessage(t *testing.T) {
	control := &computermocks.FakeScreenControl{}
	control.HeldReturns(true)
	queue := &convmocks.FakeMessageQueue{}
	panel, sink := startPanel(screenDeps(control, queue))
	woken := 0
	panel.screen.wake = func() { woken++ }

	if !handle(t, panel, map[string]any{"type": "screen_control", "id": "hreq", "action": "hand_back"}) {
		t.Fatal("the screen_control frame was left to the caller")
	}

	readFrame(t, sink, "screen_result")
	if control.HandBackCallCount() != 1 {
		t.Fatal("the screen was not handed back")
	}
	if queue.EnqueueCallCount() != 1 {
		t.Fatalf("the hand-back queued %d messages, want the continue one", queue.EnqueueCallCount())
	}
	_, message, source, requestID := queue.EnqueueArgsForCall(0)
	if source != convdomain.QueueSourceStdin || requestID != ipc.UserMessageRequestID {
		t.Fatalf("the continue message landed as %s/%s, want a run input", source, requestID)
	}
	if text := formatting.ExtractTextFromContent(message.Content, nil); text != handBackMessage {
		t.Fatalf("the queued message = %q, want %q", text, handBackMessage)
	}
	if woken != 1 {
		t.Fatal("the turn loop stayed asleep after the hand-back's message")
	}
}

func TestPanelScreenHandBackWhenNotHeldLeavesTheQueue(t *testing.T) {
	control := &computermocks.FakeScreenControl{}
	queue := &convmocks.FakeMessageQueue{}
	panel, _ := startPanel(screenDeps(control, queue))
	woken := 0
	panel.screen.wake = func() { woken++ }

	if !handle(t, panel, map[string]any{"type": "screen_control", "id": "hreq", "action": "hand_back"}) {
		t.Fatal("the screen_control frame was left to the caller")
	}

	if queue.EnqueueCallCount() != 0 || woken != 0 {
		t.Fatalf("an idle hand-back queued %d messages and woke %d times", queue.EnqueueCallCount(), woken)
	}
}

func TestPanelScreenInputDrivesTheHeldScreen(t *testing.T) {
	control := &computermocks.FakeScreenControl{}
	control.HeldReturns(true)
	panel, sink := startPanel(screenDeps(control, nil))

	if !handle(t, panel, map[string]any{"type": "screen_input", "id": "ireq", "input": map[string]any{"kind": "click", "x": 64, "y": 32, "button": "right"}}) {
		t.Fatal("the screen_input frame was left to the caller")
	}

	frame := readFrame(t, sink, "screen_result")
	if frame["held"] != true {
		t.Fatalf("screen_result = %v, want the held screen", frame)
	}
	if control.DriveCallCount() != 1 {
		t.Fatalf("the person's input drove the screen %d times, want once", control.DriveCallCount())
	}
	_, input := control.DriveArgsForCall(0)
	if input.Kind != computerdomain.ActionClick || input.Button != "right" {
		t.Fatalf("the driven input = %+v, want the right click", input)
	}
}

func TestPanelScreenInputRefusedWhenNotHeld(t *testing.T) {
	control := &computermocks.FakeScreenControl{}
	panel, sink := startPanel(screenDeps(control, nil))

	if !handle(t, panel, map[string]any{"type": "screen_input", "id": "ireq", "input": map[string]any{"kind": "click", "x": 1, "y": 2}}) {
		t.Fatal("the screen_input frame was left to the caller")
	}

	frame := readFrame(t, sink, "screen_result")
	if frame["held"] != false || !strings.Contains(errorText(frame), "not held") {
		t.Fatalf("screen_result = %v, want the not-held refusal", frame)
	}
	if control.DriveCallCount() != 0 {
		t.Fatal("a stale tab drove a screen it handed back")
	}
}

func TestPanelScreenWithoutAScreenControl(t *testing.T) {
	panel, sink := startPanel(panelDeps())

	if !handle(t, panel, map[string]any{"type": "screen_control", "id": "sreq", "action": "take_over"}) {
		t.Fatal("the screen_control frame was left to the caller")
	}

	frame := readFrame(t, sink, "screen_result")
	if !strings.Contains(errorText(frame), "no screen control") {
		t.Fatalf("screen_result = %v, want the no-control refusal", frame)
	}
}

func TestPanelScreenUnknownAction(t *testing.T) {
	control := &computermocks.FakeScreenControl{}
	panel, sink := startPanel(screenDeps(control, nil))

	if !handle(t, panel, map[string]any{"type": "screen_control", "id": "sreq", "action": "seize"}) {
		t.Fatal("the screen_control frame was left to the caller")
	}

	frame := readFrame(t, sink, "screen_result")
	if !strings.Contains(errorText(frame), "unknown screen action") {
		t.Fatalf("screen_result = %v, want the unknown-action refusal", frame)
	}
}
