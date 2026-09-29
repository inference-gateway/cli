package headless

import (
	"strings"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	conversationmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

func newTestControl() (*headlessControl, *agentdomainmocks.FakeAgentService, *statemanager.Store) {
	agent := &agentdomainmocks.FakeAgentService{}
	sm := statemanager.NewStore(false)
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
	if msg, source, reqID := queue.EnqueueArgsForCall(0); source != convdomain.QueueSourceStdin || reqID != ipc.UserMessageRequestID || msg.Role != sdk.User {
		t.Fatalf("enqueued (%+v, %q, %q), want user role tagged %q", msg, source, reqID, ipc.UserMessageRequestID)
	}

	ctl.dispatchLine([]byte(`{"type":"user_message","content":""}`))
	if queue.EnqueueCallCount() != 1 {
		t.Fatal("empty user_message landed on the message queue")
	}
}

func TestUIBridge_ForwardsRecordingStatusIntoStream(t *testing.T) {
	bridge := make(uiBridge, 1)
	bridge.Notify(agentdomain.BrowserExtensionStatusEvent{Connected: true})
	bridge.Notify(agentdomain.ScreenRecordingStatusEvent{Active: true})
	bridge.Notify(agentdomain.ScreenRecordingStatusEvent{Active: false})

	events := make(chan agentdomain.ChatEvent)
	merged := bridge.merge(events)

	ev, ok := recvEvent(t, merged).(agentdomain.ScreenRecordingStatusEvent)
	if !ok || !ev.Active || ev.Timestamp.IsZero() {
		t.Fatalf("first merged event = %#v, want stamped ScreenRecordingStatusEvent{Active: true}", ev)
	}

	events <- agentdomain.ChatChunkEvent{Content: "hi"}
	if _, ok := recvEvent(t, merged).(agentdomain.ChatChunkEvent); !ok {
		t.Fatal("stream event not forwarded, or the full-buffer notification was not dropped")
	}

	close(events)
	select {
	case _, ok := <-merged:
		if ok {
			t.Fatal("merged stream still open after events closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("merged stream did not close after events closed")
	}
}

func TestHeadlessControl_ServeFrames(t *testing.T) {
	ctl, agent, _ := newTestControl()
	frames := make(frameSink, 1)
	ctl.browser = newStdioBrowser(frames)

	ctl.dispatchLine([]byte(`{"type":"user_message","content":"next turn"}`))
	select {
	case <-ctl.wake:
	default:
		t.Fatal("user_message must wake the serve loop")
	}

	ctl.dispatchLine([]byte(`{"type":"interrupt"}`))
	if agent.CancelRequestCallCount() != 1 || agent.CancelRequestArgsForCall(0) != "sess-1" {
		t.Fatalf("interrupt must cancel the session request, got %d calls", agent.CancelRequestCallCount())
	}

	result := make(chan string, 1)
	go func() {
		raw, _ := ctl.browser.Request(t.Context(), "cmd-1", []byte(`{"type":"browser_command","id":"cmd-1"}`))
		result <- string(raw)
	}()
	<-frames
	line := []byte(`{"type":"browser_result","id":"cmd-1","title":"Example"}`)
	want := string(line)
	ctl.dispatchLine(line)
	copy(line, "reused by the next scan")
	if got := <-result; got != want {
		t.Fatalf("browser_result = %s, want %s untouched by the reader reusing its buffer", got, want)
	}
}

func TestHeadlessControl_AwaitTurn(t *testing.T) {
	t.Run("queued message starts a turn", func(t *testing.T) {
		ctl, _, _ := newTestControl()
		if !ctl.awaitTurn() {
			t.Fatal("awaitTurn() = false with a message already queued")
		}
	})

	t.Run("idle frames are dropped until a message lands", func(t *testing.T) {
		ctl, _, _ := newTestControl()
		queue := ctl.messageQueue.(*conversationmocks.FakeMessageQueue)
		queue.IsEmptyReturnsOnCall(0, true)
		queue.IsEmptyReturnsOnCall(1, true)
		queue.IsEmptyReturnsOnCall(2, true)
		queue.IsEmptyReturnsOnCall(3, false)
		ctl.approvals <- ipc.ApprovalResponse{ToolCallID: "stale"}
		ctl.ctrlEvents <- agentdomain.ComputerUsePausedEvent{RequestID: "sess-1"}
		ctl.wake <- struct{}{}
		if !ctl.awaitTurn() {
			t.Fatal("awaitTurn() = false, want a turn once the message landed")
		}
		if len(ctl.approvals) != 0 || len(ctl.ctrlEvents) != 0 {
			t.Fatal("frames that arrived between turns must be drained, not left for the next run")
		}
	})

	t.Run("stdin EOF with an empty queue ends the worker", func(t *testing.T) {
		ctl, _, _ := newTestControl()
		ctl.messageQueue.(*conversationmocks.FakeMessageQueue).IsEmptyReturns(true)
		go ctl.readLines(strings.NewReader(""))
		if ctl.awaitTurn() {
			t.Fatal("awaitTurn() = true after stdin EOF with nothing queued")
		}
	})
}

func TestHeadlessControl_PanelFramesStayOffTheTurnChannels(t *testing.T) {
	ctl, _, sm := newTestControl()
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{Success: true, Data: &agentdomain.BashToolResult{Output: "hi\n"}}, nil)
	approval := &agentdomainmocks.FakeApprovalPolicy{}
	approval.ShouldRequireApprovalReturns(true)
	frames := make(frameSink, 8)
	ctl.panel = agui.NewPanel(agui.PanelDeps{
		Conversations: &conversationmocks.FakeConversationRepository{},
		Skills:        &agentdomainmocks.FakeSkillsService{},
		Tools:         tools,
		Approval:      approval,
		Models:        &conversationmocks.FakeModelService{},
		Modes:         sm,
	}, frames)

	ctl.dispatchLine([]byte(`{"type":"tool_request","id":"req-1","tool_name":"Bash","tool_args":"{}"}`))
	if frame := <-frames; !strings.Contains(frame, `"approval_request"`) || !strings.Contains(frame, `"tool_call_id":"req-1"`) {
		t.Fatalf("expected the tool_request's approval_request, got %s", frame)
	}
	ctl.dispatchLine([]byte(`{"type":"approval_response","tool_call_id":"req-1","approved":true}`))
	if frame := <-frames; !strings.Contains(frame, `"tool_result"`) || !strings.Contains(frame, `"success":true`) {
		t.Fatalf("expected a successful tool_result, got %s", frame)
	}
	select {
	case resp := <-ctl.approvals:
		t.Fatalf("the panel's approval leaked to the turn: %+v", resp)
	default:
	}

	ctl.dispatchLine([]byte(`{"type":"approval_response","tool_call_id":"agent-call","approved":true}`))
	select {
	case resp := <-ctl.approvals:
		if resp.ToolCallID != "agent-call" {
			t.Fatalf("approval = %+v, want agent-call", resp)
		}
	default:
		t.Fatal("an agent approval must still reach the running turn")
	}
}

func TestHeadlessControl_UserMessageAttachments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctl, _, _ := newTestControl()
	queue := ctl.messageQueue.(*conversationmocks.FakeMessageQueue)

	ctl.dispatchLine([]byte(`{"type":"user_message","content":"look at these","attachments":[` +
		`{"filename":"shot.png","mime_type":"image/png","data":"iVBORw0KGgo="},` +
		`{"filename":"../../notes.txt","mime_type":"text/plain","data":"aGVsbG8="}]}`))
	if queue.EnqueueCallCount() != 1 {
		t.Fatalf("enqueue calls = %d, want 1", queue.EnqueueCallCount())
	}
	msg, _, _ := queue.EnqueueArgsForCall(0)
	parts, err := msg.Content.AsMessageContent1()
	if err != nil || len(parts) != 2 {
		t.Fatalf("expected a text part and an image part, got %d parts (%v)", len(parts), err)
	}
	text, err := parts[0].AsTextContentPart()
	if err != nil || !strings.Contains(text.Text, "look at these") || !strings.Contains(text.Text, "[notes.txt saved at ") {
		t.Fatalf("text part = %q (%v), want the content plus a note naming the saved file", text.Text, err)
	}
}
