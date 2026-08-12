package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

type notifierFakeChannel struct {
	mu       sync.Mutex
	name     string
	received []domain.OutboundMessage
}

func (f *notifierFakeChannel) Name() string { return f.name }
func (f *notifierFakeChannel) Start(_ context.Context, _ chan<- domain.InboundMessage) error {
	return nil
}
func (f *notifierFakeChannel) Stop() error { return nil }
func (f *notifierFakeChannel) Send(_ context.Context, m domain.OutboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.received = append(f.received, m)
	return nil
}

func (f *notifierFakeChannel) Snapshot() []domain.OutboundMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.OutboundMessage, len(f.received))
	copy(out, f.received)
	return out
}

func newNotifierWithChannel(ch *notifierFakeChannel) *ScheduleNotifier {
	return NewScheduleNotifier(func(name string) domain.Channel {
		if ch != nil && name == ch.name {
			return ch
		}
		return nil
	})
}

func TestScheduleNotifier_DeliversAssistantLines(t *testing.T) {
	ch := &notifierFakeChannel{name: "telegram"}
	n := newNotifierWithChannel(ch)
	job := domain.ScheduledJob{ID: "j1", Channel: "telegram", RecipientID: "user1"}

	n.Notify(job, domain.RunEvent{Line: []byte(`{"role":"assistant","content":"hello"}`)})

	got := ch.Snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].ChannelName != "telegram" || got[0].RecipientID != "user1" || got[0].Content != "hello" {
		t.Fatalf("wrong message: %+v", got[0])
	}
}

func TestScheduleNotifier_RecordOnlyJobIsIgnored(t *testing.T) {
	ch := &notifierFakeChannel{name: "telegram"}
	n := newNotifierWithChannel(ch)
	job := domain.ScheduledJob{ID: "j1"}

	n.Notify(job, domain.RunEvent{Line: []byte(`{"role":"assistant","content":"hello"}`)})
	n.Notify(job, domain.RunEvent{Done: true, Err: errors.New("boom")})

	if got := ch.Snapshot(); len(got) != 0 {
		t.Fatalf("expected no delivery for record-only job, got %d messages", len(got))
	}
}

func TestScheduleNotifier_FailureEvent(t *testing.T) {
	ch := &notifierFakeChannel{name: "telegram"}
	n := newNotifierWithChannel(ch)
	job := domain.ScheduledJob{ID: "j1", Name: "morning report", Channel: "telegram", RecipientID: "user1"}

	n.Notify(job, domain.RunEvent{Done: true, Err: errors.New("agent exploded")})

	got := ch.Snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 failure message, got %d", len(got))
	}
	if !strings.Contains(got[0].Content, "morning report") || !strings.Contains(got[0].Content, "agent exploded") {
		t.Fatalf("failure message missing details: %q", got[0].Content)
	}
}

func TestScheduleNotifier_UnregisteredChannelIsSkipped(t *testing.T) {
	n := newNotifierWithChannel(nil)
	job := domain.ScheduledJob{ID: "j1", Channel: "telegram", RecipientID: "user1"}
	// Must not panic on nil channel lookup.
	n.Notify(job, domain.RunEvent{Line: []byte(`{"role":"assistant","content":"hello"}`)})
}
