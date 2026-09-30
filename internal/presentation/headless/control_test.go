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

func newTestControl() (*headlessControl, *agentdomainmocks.FakeAgentService, *conversationmocks.FakeMessageQueue) {
	agent := &agentdomainmocks.FakeAgentService{}
	queue := &conversationmocks.FakeMessageQueue{}
	return newHeadlessControl(agent, queue, "sess-1"), agent, queue
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

func TestHeadlessControl_BrokerFrames(t *testing.T) {
	ctl, agent, _ := newTestControl()

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

	for _, noise := range []string{"not json", `{"type":"other"}`, `{"type":"browser_result","id":"unknown"}`} {
		ctl.dispatchLine([]byte(noise))
	}
	select {
	case resp := <-ctl.approvals:
		t.Fatalf("noise produced an approval response %+v", resp)
	default:
	}
	if agent.CancelRequestCallCount() != 0 {
		t.Fatal("noise cancelled the session request")
	}
}

func TestHeadlessControl_RunInputMessages(t *testing.T) {
	ctl, _, queue := newTestControl()

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"threadId":"t1","messages":[{"role":"user","content":"finally open it"}]}}`))
	if queue.EnqueueCallCount() != 1 {
		t.Fatalf("run input enqueue calls = %d, want 1", queue.EnqueueCallCount())
	}
	msg, source, reqID := queue.EnqueueArgsForCall(0)
	if source != convdomain.QueueSourceStdin || reqID != ipc.UserMessageRequestID || msg.Role != sdk.User {
		t.Fatalf("enqueued (%+v, %q, %q), want user role tagged %q", msg, source, reqID, ipc.UserMessageRequestID)
	}
	select {
	case <-ctl.wake:
	case <-time.After(2 * time.Second):
		t.Fatal("a run input did not wake the serve loop")
	}

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"threadId":"t1"}}`))
	if queue.EnqueueCallCount() != 2 {
		t.Fatalf("the continue run enqueue calls = %d, want 2", queue.EnqueueCallCount())
	}
	continueMsg, _, _ := queue.EnqueueArgsForCall(1)
	text, _ := continueMsg.Content.AsMessageContent0()
	if text != continuePrompt {
		t.Fatalf("continue run enqueue = %q, want the continue prompt", text)
	}

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"messages":[{"role":"user","content":""}]}}`))
	if queue.EnqueueCallCount() != 2 {
		t.Fatal("an empty run input message landed on the message queue")
	}
}

func TestHeadlessControl_RunInputResumeRoutesToTheBrokers(t *testing.T) {
	ctl, _, _ := newTestControl()
	ctl.Note("tc1", agui.InterruptToolCall)
	ctl.Note("q1", agui.InterruptInputRequired)
	ctl.Note("gone", agui.InterruptToolCall)
	ctl.Forget("gone")

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"threadId":"t1","resume":[{"interruptId":"tc1","status":"resolved"}]}}`))
	select {
	case resp := <-ctl.approvals:
		if resp.ToolCallID != "tc1" || !resp.Approved {
			t.Fatalf("resume approval = %+v, want tc1 approved", resp)
		}
	default:
		t.Fatal("the resume entry did not reach the approval broker")
	}

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"resume":[{"interruptId":"q1","status":"resolved","payload":{"answers":[{"header":"Lang","selectedLabels":["Go"]}]}}]}}`))
	select {
	case resp := <-ctl.questions:
		if resp.ToolCallID != "q1" || resp.Cancelled || !strings.Contains(string(resp.Answers), `"Go"`) {
			t.Fatalf("resume question = %+v, want q1 with the payload answers", resp)
		}
	default:
		t.Fatal("the resume entry did not reach the question broker")
	}

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"resume":[{"interruptId":"gone","status":"resolved"}]}}`))
	select {
	case resp := <-ctl.approvals:
		t.Fatalf("a resume for a forgotten interrupt decided the broker: %+v", resp)
	default:
	}

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"resume":[{"interruptId":"tc1","status":"cancelled"}]}}`))
	select {
	case resp := <-ctl.approvals:
		if resp.Approved {
			t.Fatalf("a cancelled resume entry approved the call: %+v", resp)
		}
	default:
		t.Fatal("a cancelled resume entry did not reject the approval")
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

// TestUIBridge_ForwardsComputerUseActionIntoStream: the Computer tool's
// activity notifications join the rendered stream like the recorder's do, and
// unrelated notifications stay unbridged.
func TestUIBridge_ForwardsComputerUseActionIntoStream(t *testing.T) {
	bridge := make(uiBridge, 1)
	bridge.Notify(agentdomain.ModelSelectedEvent{Model: "m"})
	bridge.Notify(agentdomain.ComputerUseActionEvent{ToolCallID: "tc1", Action: "click", X: 640, Y: 512, ScreenWidth: 1920, ScreenHeight: 1080})

	events := make(chan agentdomain.ChatEvent)
	merged := bridge.merge(events)

	ev, ok := recvEvent(t, merged).(agentdomain.ComputerUseActionEvent)
	if !ok || ev.Action != "click" || ev.X != 640 || ev.Timestamp.IsZero() {
		t.Fatalf("first merged event = %#v, want the stamped computer-use action", ev)
	}
	close(events)
}

func TestHeadlessControl_ServeFrames(t *testing.T) {
	ctl, agent, _ := newTestControl()
	frames := make(frameSink, 1)
	ctl.browser = newStdioBrowser(frames)

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"threadId":"t1","messages":[{"role":"user","content":"next turn"}]}}`))
	select {
	case <-ctl.wake:
	default:
		t.Fatal("run_agent_input must wake the serve loop")
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
		ctl.questions <- ipc.UserQuestionResponse{ToolCallID: "stale"}
		ctl.wake <- struct{}{}
		if !ctl.awaitTurn() {
			t.Fatal("awaitTurn() = false, want a turn once the message landed")
		}
		if len(ctl.approvals) != 0 || len(ctl.questions) != 0 {
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
	ctl, _, _ := newTestControl()
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{Success: true, Data: &agentdomain.BashToolResult{Output: "hi\n"}}, nil)
	approval := &agentdomainmocks.FakeApprovalPolicy{}
	approval.ShouldRequireApprovalReturns(true)
	frames := make(frameSink, 8)
	ctl.panel = NewPanel(PanelDeps{
		Conversations: &conversationmocks.FakeConversationRepository{},
		Skills:        &agentdomainmocks.FakeSkillsService{},
		Tools:         tools,
		Approval:      approval,
		Models:        &conversationmocks.FakeModelService{},
		Modes:         statemanager.NewStore(false),
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

func TestHeadlessControl_RunInputAttachments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctl, _, queue := newTestControl()

	ctl.dispatchLine([]byte(`{"type":"run_agent_input","input":{"messages":[{"role":"user","content":[` +
		`{"type":"text","text":"look at these"},` +
		`{"type":"image","filename":"shot.png","mimeType":"image/png","data":"iVBORw0KGgo="},` +
		`{"type":"image","filename":"../../notes.txt","mimeType":"text/plain","data":"aGVsbG8="}]}]}}`))
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
