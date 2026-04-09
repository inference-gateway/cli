package services

import (
	"context"
	"os/exec"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// mockChannel is a test double for domain.Channel
type mockChannel struct {
	name     string
	started  bool
	stopped  bool
	sent     []domain.OutboundMessage
	startFn  func(ctx context.Context, inbox chan<- domain.InboundMessage) error
	sendFn   func(ctx context.Context, msg domain.OutboundMessage) error
	startErr error
}

func (m *mockChannel) Name() string { return m.name }

func (m *mockChannel) Start(ctx context.Context, inbox chan<- domain.InboundMessage) error {
	m.started = true
	if m.startFn != nil {
		return m.startFn(ctx, inbox)
	}
	if m.startErr != nil {
		return m.startErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	m.sent = append(m.sent, msg)
	if m.sendFn != nil {
		return m.sendFn(ctx, msg)
	}
	return nil
}

func (m *mockChannel) Stop() error {
	m.stopped = true
	return nil
}

func TestChannelManagerService_Register(t *testing.T) {
	cfg := config.ChannelsConfig{Enabled: true}
	cm := NewChannelManagerService(cfg)

	ch := &mockChannel{name: "test"}
	cm.Register(ch)

	cm.mu.RLock()
	_, exists := cm.channels["test"]
	cm.mu.RUnlock()

	if !exists {
		t.Fatal("expected channel to be registered")
	}
}

func TestChannelManagerService_StartDisabled(t *testing.T) {
	cfg := config.ChannelsConfig{Enabled: false}
	cm := NewChannelManagerService(cfg)

	err := cm.Start(context.Background())
	if err != nil {
		t.Fatalf("expected no error when disabled, got %v", err)
	}
}

func TestChannelManagerService_StopChannels(t *testing.T) {
	cfg := config.ChannelsConfig{Enabled: true}
	cm := NewChannelManagerService(cfg)

	ch := &mockChannel{name: "test"}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cm.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	err = cm.Stop()
	if err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}

	if !ch.stopped {
		t.Fatal("expected channel to be stopped")
	}
}

func TestChannelManagerService_IsAllowedUser(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123", "456"},
		},
	}
	cm := NewChannelManagerService(cfg)

	tests := []struct {
		channel  string
		senderID string
		want     bool
	}{
		{"telegram", "123", true},
		{"telegram", "456", true},
		{"telegram", "789", false},
		{"unknown", "123", false},
	}

	for _, tt := range tests {
		got := cm.isAllowedUser(tt.channel, tt.senderID)
		if got != tt.want {
			t.Errorf("isAllowedUser(%q, %q) = %v, want %v", tt.channel, tt.senderID, got, tt.want)
		}
	}
}

func TestChannelManagerService_IsAllowedUser_EmptyList(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{},
		},
	}
	cm := NewChannelManagerService(cfg)

	// Empty allowed list = reject all (secure by default)
	if cm.isAllowedUser("telegram", "123") {
		t.Fatal("expected rejection with empty allowed list")
	}
}

func TestChannelManagerService_InboundRouting(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg)

	// Override exec to return a mock agent response
	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"Hello from agent!","timestamp":"2024-01-01T00:00:00Z"}`)
	}

	responseSent := make(chan domain.OutboundMessage, 1)
	ch := &mockChannel{
		name: "telegram",
		startFn: func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
			inbox <- domain.InboundMessage{
				ChannelName: "telegram",
				SenderID:    "123",
				Content:     "hello agent",
				Timestamp:   time.Now(),
			}
			<-ctx.Done()
			return ctx.Err()
		},
		sendFn: func(ctx context.Context, msg domain.OutboundMessage) error {
			responseSent <- msg
			return nil
		},
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cm.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for the response to be sent back through the channel
	select {
	case msg := <-responseSent:
		if msg.Content != "Hello from agent!" {
			t.Errorf("expected 'Hello from agent!', got %q", msg.Content)
		}
		if msg.RecipientID != "123" {
			t.Errorf("expected recipient '123', got %q", msg.RecipientID)
		}
		if msg.ChannelName != "telegram" {
			t.Errorf("expected channel 'telegram', got %q", msg.ChannelName)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestChannelManagerService_StreamingMultipleMessages(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg)

	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf",
			`{"role":"assistant","content":"Let me check...","tools":["Read"]}\n{"role":"tool","content":"file contents","tool_call_id":"123"}\n{"role":"assistant","content":"Here are the results."}`)
	}

	var messages []domain.OutboundMessage
	allSent := make(chan struct{}, 1)
	ch := &mockChannel{
		name: "telegram",
		startFn: func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
			inbox <- domain.InboundMessage{
				ChannelName: "telegram",
				SenderID:    "123",
				Content:     "read my files",
				Timestamp:   time.Now(),
			}
			<-ctx.Done()
			return ctx.Err()
		},
		sendFn: func(ctx context.Context, msg domain.OutboundMessage) error {
			messages = append(messages, msg)
			if len(messages) == 2 {
				allSent <- struct{}{}
			}
			return nil
		},
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-allSent:
		if len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(messages))
		}
		if messages[0].Content != "Let me check...\n\n🔧 Using tool: Read" {
			t.Errorf("expected tool message, got %q", messages[0].Content)
		}
		if messages[1].Content != "Here are the results." {
			t.Errorf("expected final answer, got %q", messages[1].Content)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for messages, got %d", len(messages))
	}
}

func TestChannelManagerService_UnauthorizedUserRejected(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"allowed_user"},
		},
	}
	cm := NewChannelManagerService(cfg)

	// If exec is called, the test should fail — unauthorized messages should not trigger agent
	agentCalled := false
	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		agentCalled = true
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"should not happen"}`)
	}

	inboxSent := make(chan struct{}, 1)
	ch := &mockChannel{
		name: "telegram",
		startFn: func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
			inbox <- domain.InboundMessage{
				ChannelName: "telegram",
				SenderID:    "unauthorized_user",
				Content:     "should be rejected",
				Timestamp:   time.Now(),
			}
			inboxSent <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cm.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-inboxSent:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbox message")
	}

	// Give router time to process (and reject)
	time.Sleep(100 * time.Millisecond)

	if agentCalled {
		t.Fatal("agent should not have been called for unauthorized user")
	}
}

func TestFormatAgentMessage(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "assistant text message",
			line: `{"role":"assistant","content":"Hello!"}`,
			want: "Hello!",
		},
		{
			name: "assistant with tool calls",
			line: `{"role":"assistant","content":"Let me check...","tools":["Read","Grep"]}`,
			want: "Let me check...\n\n🔧 Using tool: Read, Grep",
		},
		{
			name: "assistant with tool calls no content",
			line: `{"role":"assistant","content":"","tools":["Write"]}`,
			want: "🔧 Using tool: Write",
		},
		{
			name: "tool result is skipped",
			line: `{"role":"tool","content":"file contents","tool_call_id":"123"}`,
			want: "",
		},
		{
			name: "status message is skipped",
			line: `{"type":"info","message":"Starting session"}`,
			want: "",
		},
		{
			name: "user message is skipped",
			line: `{"role":"user","content":"hello"}`,
			want: "",
		},
		{
			name: "malformed JSON is skipped",
			line: `not json`,
			want: "",
		},
		{
			name: "empty assistant content is skipped",
			line: `{"role":"assistant","content":""}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAgentMessage([]byte(tt.line))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChannelManagerService_SessionID(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg)

	// Capture the session ID passed to the agent
	var capturedArgs []string
	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"ok"}`)
	}

	responseSent := make(chan struct{}, 1)
	ch := &mockChannel{
		name: "telegram",
		startFn: func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
			inbox <- domain.InboundMessage{
				ChannelName: "telegram",
				SenderID:    "123",
				Content:     "test message",
				Timestamp:   time.Now(),
			}
			<-ctx.Done()
			return ctx.Err()
		},
		sendFn: func(ctx context.Context, msg domain.OutboundMessage) error {
			responseSent <- struct{}{}
			return nil
		},
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-responseSent:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Verify session ID format: agent --session-id channel-telegram-123 "test message"
	expectedSessionID := "channel-telegram-123"
	found := false
	for i, arg := range capturedArgs {
		if arg == "--session-id" && i+1 < len(capturedArgs) && capturedArgs[i+1] == expectedSessionID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected session ID %q in args, got %v", expectedSessionID, capturedArgs)
	}
}
