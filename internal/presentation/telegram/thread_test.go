package telegram

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	channelmocks "github.com/inference-gateway/cli/tests/mocks/channels"
	sessionmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	config "github.com/inference-gateway/cli/config"
	channels "github.com/inference-gateway/cli/internal/channels"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// newRenderChat builds the sender's render adapter against a fake router,
// wired the way the daemon wires the manager, recording the chat's
// outbound messages over a channel.
func newRenderChat(t *testing.T, cm *ChannelManagerService, ch *channelmocks.FakeChannel, sent chan channels.OutboundMessage) *threadChat {
	t.Helper()
	ch.NameReturns("telegram")
	if sent != nil {
		ch.SendStub = func(_ context.Context, msg channels.OutboundMessage) error {
			sent <- msg
			return nil
		}
	}
	return cm.threadChatFor(context.Background(), "telegram-123", ch, "123", "/proj")
}

// Deliver routes one inbound message to the chat's thread: the resume
// frame that keeps it following the deterministic session id per sender
// on the daemon's working dir, then the run_agent_input that starts the
// next run with the message.
func TestThreadChat_DeliverUserMessage(t *testing.T) {
	cm := NewChannelManagerService(config.ChannelsConfig{Enabled: true}, nil)
	router := &sessionmocks.FakeThreadRouter{}
	projectDir := t.TempDir()
	cm.SetThreadDriver(router, projectDir, sessionsdomain.ThreadOptions{SystemPrompt: "You are remote controlled."})
	chat := newRenderChat(t, cm, &channelmocks.FakeChannel{}, nil)

	err := chat.deliverUserMessage(context.Background(), channels.InboundMessage{
		ChannelName: "telegram",
		SenderID:    "123",
		Content:     "look at this",
	}, projectDir, sessionsdomain.ThreadOptions{SystemPrompt: "You are remote controlled."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if router.HandleCallCount() != 2 {
		t.Fatalf("expected the resume and the message frames, got %d", router.HandleCallCount())
	}

	_, frame := router.HandleArgsForCall(0)
	var resume map[string]any
	if err := json.Unmarshal(frame, &resume); err != nil {
		t.Fatalf("undecodable frame %s: %v", frame, err)
	}
	if resume["type"] != "resume_conversation" {
		t.Fatalf("expected the follow-the-thread frame first, got %s", frame)
	}
	if resume["id"] != "channel-telegram-123" {
		t.Errorf("expected the deterministic session id per sender, got %v", resume["id"])
	}
	if resume["project_dir"] != projectDir {
		t.Errorf("expected the thread in the daemon's working dir %q, got %v", projectDir, resume["project_dir"])
	}
	if resume["system_prompt"] != "You are remote controlled." {
		t.Errorf("expected the thread options applied when the worker launches, got %v", resume["system_prompt"])
	}

	_, frame = router.HandleArgsForCall(1)
	var input runInputFrame
	if err := json.Unmarshal(frame, &input); err != nil {
		t.Fatalf("undecodable frame %s: %v", frame, err)
	}
	if input.Type != "run_agent_input" || input.Input.ThreadID != "channel-telegram-123" || len(input.Input.Messages) != 1 {
		t.Fatalf("expected one run input on the chat's thread, got %s", frame)
	}
	if msg := input.Input.Messages[0]; msg.Role != "user" || msg.Content != "look at this" {
		t.Errorf("expected the message as the run's one user message, got %s", frame)
	}
}

// A suspended run's interrupts are answered with one run input of resume
// entries: the tool approval the user gave, keyed by the call the run
// streamed, and a question dismissed.
func TestThreadChat_AnswersInterrupts(t *testing.T) {
	cm := NewChannelManagerService(config.ChannelsConfig{Enabled: true, RequireApproval: true}, nil)
	router := &sessionmocks.FakeThreadRouter{}
	cm.SetThreadDriver(router, t.TempDir(), sessionsdomain.ThreadOptions{})
	ch := &channelmocks.FakeChannel{}
	sent := make(chan channels.OutboundMessage, 4)
	chat := newRenderChat(t, cm, ch, sent)

	chat.handle([]byte(`{"type":"TOOL_CALL_START","toolCallId":"call-1","toolCallName":"Bash"}`))
	chat.handle([]byte(`{"type":"TOOL_CALL_ARGS","toolCallId":"call-1","delta":"{\"command\":\"ls\"}"}`))
	chat.handle([]byte(`{"type":"TOOL_CALL_END","toolCallId":"call-1"}`))
	go func() {
		prompt := <-sent
		if !strings.Contains(prompt.Content, "ls") {
			t.Errorf("expected the approval prompt to name the command, got %q", prompt.Content)
		}
		respChan, ok := cm.pendingApprovals.Load("telegram-123")
		if !ok {
			t.Error("expected the approval pending under the sender's key while the prompt is out")
			return
		}
		deliverApprovalReply(respChan, true)
	}()
	chat.handle([]byte(`{"type":"RUN_FINISHED","threadId":"t","runId":"r","outcome":{"type":"interrupt","interrupts":[` +
		`{"id":"call-1","reason":"tool_call","toolCallId":"call-1"},{"id":"q-1","reason":"input_required"}]}}`))

	if router.HandleCallCount() != 1 {
		t.Fatalf("expected one resume frame, got %d", router.HandleCallCount())
	}
	_, frame := router.HandleArgsForCall(0)
	var input runInputFrame
	if err := json.Unmarshal(frame, &input); err != nil {
		t.Fatalf("undecodable frame %s: %v", frame, err)
	}
	want := []agui.ResumeEntry{{InterruptID: "call-1", Status: agui.ResumeStatusResolved}, {InterruptID: "q-1", Status: agui.ResumeStatusCancelled}}
	if len(input.Input.Messages) != 0 || !reflect.DeepEqual(input.Input.Resume, want) {
		t.Errorf("expected the resume entries %+v and no messages, got %s", want, frame)
	}
}

func TestThreadChat_ArmIdleDetach(t *testing.T) {
	cm := NewChannelManagerService(config.ChannelsConfig{Enabled: true}, nil)
	router := &sessionmocks.FakeThreadRouter{}
	cm.SetThreadDriver(router, t.TempDir(), sessionsdomain.ThreadOptions{})
	chat := newRenderChat(t, cm, &channelmocks.FakeChannel{}, nil)

	chat.mu.Lock()
	chat.active = time.Now().Add(-2 * sessionsdomain.IdleTimeout) // backdate under the lock
	chat.mu.Unlock()

	chat.detachIdle() // the grace expired while the chat sat silent

	if router.DetachCallCount() != 1 {
		t.Fatalf("expected the idle chat to stop following its thread once, got %d", router.DetachCallCount())
	}
}
