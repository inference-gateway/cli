package telegram

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	channelmocks "github.com/inference-gateway/cli/tests/mocks/channels"
	sessionmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	config "github.com/inference-gateway/cli/config"
	channels "github.com/inference-gateway/cli/internal/channels"
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
	return cm.threadChatFor(context.Background(), "telegram-123", ch, "123")
}

// newRouterChatwiring builds a chat whose frames land on a fake router
// outside the manager, for tests driving the render functions directly.
func newRouterChat(t *testing.T, cm *ChannelManagerService) (*threadChat, *sessionmocks.FakeThreadRouter) {
	t.Helper()
	router := &sessionmocks.FakeThreadRouter{}
	cm.SetThreadDriver(router, t.TempDir(), sessionsdomain.ThreadOptions{})
	return newRenderChat(t, cm, &channelmocks.FakeChannel{}, nil), router
}

// Deliver routes one inbound message to the chat's thread: the resume
// frame that keeps it following the deterministic session id per sender
// on the daemon's working dir, then the user_message that starts the
// next turn with the images as attachments.
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
	var userMsg map[string]any
	if err := json.Unmarshal(frame, &userMsg); err != nil {
		t.Fatalf("undecodable frame %s: %v", frame, err)
	}
	if userMsg["type"] != "user_message" || userMsg["content"] != "look at this" {
		t.Errorf("expected the message on its thread, got %s", frame)
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