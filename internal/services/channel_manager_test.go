package services

import (
	"context"
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
	mq := NewMessageQueueService()
	cm := NewChannelManagerService(cfg, mq)

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
	mq := NewMessageQueueService()
	cm := NewChannelManagerService(cfg, mq)

	err := cm.Start(context.Background())
	if err != nil {
		t.Fatalf("expected no error when disabled, got %v", err)
	}
}

func TestChannelManagerService_StopChannels(t *testing.T) {
	cfg := config.ChannelsConfig{Enabled: true}
	mq := NewMessageQueueService()
	cm := NewChannelManagerService(cfg, mq)

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
	mq := NewMessageQueueService()
	cm := NewChannelManagerService(cfg, mq)

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
	mq := NewMessageQueueService()
	cm := NewChannelManagerService(cfg, mq)

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
	mq := NewMessageQueueService()
	cm := NewChannelManagerService(cfg, mq)

	inboxSent := make(chan struct{}, 1)
	ch := &mockChannel{
		name: "telegram",
		startFn: func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
			inbox <- domain.InboundMessage{
				ChannelName: "telegram",
				SenderID:    "123",
				Content:     "hello agent",
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

	// Wait for message to be sent to inbox
	select {
	case <-inboxSent:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbox message")
	}

	// Give the router time to process
	time.Sleep(100 * time.Millisecond)

	if mq.IsEmpty() {
		t.Fatal("expected message to be enqueued")
	}

	msg := mq.Dequeue()
	if msg == nil {
		t.Fatal("expected non-nil message")
	}

	content, err := msg.Message.Content.AsMessageContent0()
	if err != nil {
		t.Fatalf("unexpected error getting content: %v", err)
	}
	if content != "hello agent" {
		t.Errorf("expected 'hello agent', got %q", content)
	}
}

func TestChannelManagerService_UnauthorizedUserRejected(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"allowed_user"},
		},
	}
	mq := NewMessageQueueService()
	cm := NewChannelManagerService(cfg, mq)

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

	if !mq.IsEmpty() {
		t.Fatal("expected message queue to be empty (unauthorized user)")
	}
}
