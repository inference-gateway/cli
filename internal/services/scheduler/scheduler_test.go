package scheduler

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

type fakeChannel struct {
	mu       sync.Mutex
	name     string
	received []domain.OutboundMessage
}

func (f *fakeChannel) Name() string                                                    { return f.name }
func (f *fakeChannel) Start(ctx context.Context, _ chan<- domain.InboundMessage) error { return nil }
func (f *fakeChannel) Stop() error                                                     { return nil }
func (f *fakeChannel) Send(_ context.Context, m domain.OutboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.received = append(f.received, m)
	return nil
}
func (f *fakeChannel) Snapshot() []domain.OutboundMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.OutboundMessage, len(f.received))
	copy(out, f.received)
	return out
}

func TestParseCron(t *testing.T) {
	if err := ParseCron("0 8 * * *"); err != nil {
		t.Fatalf("valid cron rejected: %v", err)
	}
	if err := ParseCron("@every 1s"); err != nil {
		t.Fatalf("@every rejected: %v", err)
	}
	if err := ParseCron("not a cron"); err == nil {
		t.Fatal("expected error for bogus expression")
	}
}

func TestNewService_ValidatesDeps(t *testing.T) {
	if _, err := NewService(Options{}); err == nil {
		t.Fatal("expected error when Store is nil")
	}
	store, _ := NewStore(t.TempDir())
	if _, err := NewService(Options{Store: store}); err == nil {
		t.Fatal("expected error when ChannelLookup is nil")
	}
}

func newTestService(t *testing.T, ch *fakeChannel, fired *atomic.Int32) (*Service, *Store) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	svc, err := NewService(Options{
		Store: store,
		ChannelLookup: func(name string) domain.Channel {
			if name == ch.name {
				return ch
			}
			return nil
		},
		ExecCommand: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			fired.Add(1)
			return exec.CommandContext(ctx, "echo",
				`{"role":"assistant","content":"hello from scheduler"}`)
		},
		BinaryPath: "/usr/bin/true",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, store
}

func TestService_Start_LoadsExistingJobs(t *testing.T) {
	ch := &fakeChannel{name: "telegram"}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, ch, fired)

	job := &domain.ScheduledJob{
		ID:             "preexisting",
		CronExpression: "@every 1s",
		Prompt:         "test",
		Channel:        "telegram",
		RecipientID:    "user1",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Save(job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	ids := svc.JobIDs()
	if len(ids) != 1 || ids[0] != "preexisting" {
		t.Fatalf("expected [preexisting], got %v", ids)
	}
}

func TestService_Fire_SendsToChannel(t *testing.T) {
	ch := &fakeChannel{name: "telegram"}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, ch, fired)

	job := &domain.ScheduledJob{
		ID:             "fast",
		CronExpression: "@every 1s",
		Prompt:         "test",
		Channel:        "telegram",
		RecipientID:    "user1",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Save(job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() >= 1 && len(ch.Snapshot()) >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	got := ch.Snapshot()
	if len(got) == 0 {
		t.Fatalf("expected at least 1 outbound message, got 0 (fired=%d)", fired.Load())
	}
	if got[0].ChannelName != "telegram" || got[0].RecipientID != "user1" {
		t.Fatalf("wrong routing: %+v", got[0])
	}
	if got[0].Content != "hello from scheduler" {
		t.Fatalf("wrong content: %q", got[0].Content)
	}
}

func TestService_FsNotifyReload_AddsNewJob(t *testing.T) {
	ch := &fakeChannel{name: "telegram"}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, ch, fired)

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	if got := len(svc.JobIDs()); got != 0 {
		t.Fatalf("expected 0 jobs initially, got %d", got)
	}

	job := &domain.ScheduledJob{
		ID:             "added-later",
		CronExpression: "0 8 * * *",
		Prompt:         "morning",
		Channel:        "telegram",
		RecipientID:    "user1",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Save(job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Wait for fsnotify + debounce.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(svc.JobIDs()) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := svc.JobIDs(); len(got) != 1 || got[0] != "added-later" {
		t.Fatalf("expected [added-later] after fs change, got %v", got)
	}
}

func TestService_FsNotifyReload_RemovesJob(t *testing.T) {
	ch := &fakeChannel{name: "telegram"}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, ch, fired)

	job := &domain.ScheduledJob{
		ID:             "doomed",
		CronExpression: "0 8 * * *",
		Prompt:         "x",
		Channel:        "telegram",
		RecipientID:    "user1",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Save(job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	if got := len(svc.JobIDs()); got != 1 {
		t.Fatalf("expected 1 job, got %d", got)
	}

	if err := store.Delete("doomed"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(svc.JobIDs()) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := svc.JobIDs(); len(got) != 0 {
		t.Fatalf("expected 0 jobs after delete, got %v", got)
	}
}

type sendErrChannel struct {
	name    string
	sendErr error
	calls   atomic.Int32
}

func (s *sendErrChannel) Name() string                                                    { return s.name }
func (s *sendErrChannel) Start(ctx context.Context, _ chan<- domain.InboundMessage) error { return nil }
func (s *sendErrChannel) Stop() error                                                     { return nil }
func (s *sendErrChannel) Send(_ context.Context, _ domain.OutboundMessage) error {
	s.calls.Add(1)
	return s.sendErr
}

func TestService_Fire_SendError_RecordedInLastError(t *testing.T) {
	ch := &sendErrChannel{name: "telegram", sendErr: errors.New("invalid chat ID \"test\"")}
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	svc, err := NewService(Options{
		Store: store,
		ChannelLookup: func(name string) domain.Channel {
			if name == ch.name {
				return ch
			}
			return nil
		},
		ExecCommand: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "echo",
				`{"role":"assistant","content":"will fail to deliver"}`)
		},
		BinaryPath: "/usr/bin/true",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	job := &domain.ScheduledJob{
		ID:             "send-fails",
		CronExpression: "@every 1s",
		Prompt:         "x",
		Channel:        "telegram",
		RecipientID:    "test",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Save(job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		loaded, err := store.Load("send-fails")
		if err == nil && loaded.LastError != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	loaded, err := store.Load("send-fails")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LastError == "" {
		t.Fatal("expected LastError to be set when channel.Send fails")
	}
	if !strings.Contains(loaded.LastError, "invalid chat ID") {
		t.Fatalf("expected LastError to mention underlying send error, got %q", loaded.LastError)
	}
}

func TestService_Fire_RunOnce_DeletesAfterFire(t *testing.T) {
	ch := &fakeChannel{name: "telegram"}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, ch, fired)

	job := &domain.ScheduledJob{
		ID:             "one-shot",
		CronExpression: "@every 1s",
		Prompt:         "x",
		Channel:        "telegram",
		RecipientID:    "user1",
		RunOnce:        true,
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Save(job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := store.Load("one-shot"); errors.Is(err, ErrNotFound) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := store.Load("one-shot"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected one-off job to be deleted after fire, got err=%v", err)
	}
	if fired.Load() < 1 {
		t.Fatal("expected job to fire at least once")
	}
}

func TestService_Fire_ChannelMissing_RecordsError(t *testing.T) {
	ch := &fakeChannel{name: "telegram"}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, ch, fired)

	job := &domain.ScheduledJob{
		ID:             "wrong-channel",
		CronExpression: "@every 1s",
		Prompt:         "x",
		Channel:        "nonexistent",
		RecipientID:    "user1",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Save(job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		loaded, err := store.Load("wrong-channel")
		if err == nil && loaded.LastError != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	loaded, err := store.Load("wrong-channel")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LastError == "" {
		t.Fatal("expected LastError to be set when channel not found")
	}
}
