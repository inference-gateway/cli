package scheduler

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// eventSink records run events emitted by the scheduler.
type eventSink struct {
	mu     sync.Mutex
	events []domain.RunEvent
	lines  []string
}

func (e *eventSink) Notify(_ domain.ScheduledJob, ev domain.RunEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ev.Line != nil {
		e.lines = append(e.lines, string(ev.Line))
	}
	e.events = append(e.events, domain.RunEvent{Err: ev.Err, Done: ev.Done})
}

func (e *eventSink) Lines() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.lines))
	copy(out, e.lines)
	return out
}

func (e *eventSink) TerminalErr() (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.events {
		if ev.Done {
			return true, ev.Err
		}
	}
	return false, nil
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
	memStore := storage.NewMemoryStorage()
	if _, err := NewService(Options{Store: memStore}); err == nil {
		t.Fatal("expected error when Runs is nil")
	}
}

func newTestService(t *testing.T, sink *eventSink, fired *atomic.Int32) (*Service, *storage.MemoryStorage) {
	t.Helper()
	memStore := storage.NewMemoryStorage()
	svc, err := NewService(Options{
		Store:      memStore,
		Runs:       memStore,
		OnRunEvent: sink.Notify,
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
	return svc, memStore
}

func TestService_Start_LoadsExistingJobs(t *testing.T) {
	sink := &eventSink{}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, sink, fired)

	job := &domain.ScheduledJob{
		ID:             "preexisting",
		CronExpression: "@every 1s",
		Prompt:         "test",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob: %v", err)
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

func TestService_Fire_EmitsEventsAndRecordsRun(t *testing.T) {
	sink := &eventSink{}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, sink, fired)

	job := &domain.ScheduledJob{
		ID:             "fast",
		CronExpression: "@every 1s",
		Prompt:         "test",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if done, _ := sink.TerminalErr(); done {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	lines := sink.Lines()
	if len(lines) == 0 {
		t.Fatalf("expected at least 1 line event, got 0 (fired=%d)", fired.Load())
	}
	if lines[0] != `{"role":"assistant","content":"hello from scheduler"}` {
		t.Fatalf("wrong line content: %q", lines[0])
	}
	done, termErr := sink.TerminalErr()
	if !done || termErr != nil {
		t.Fatalf("expected clean terminal event, done=%v err=%v", done, termErr)
	}

	runs, err := store.ListRuns(context.Background(), "fast")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected a run record to be persisted")
	}
	if runs[0].Status != domain.RunStatusCompleted {
		t.Fatalf("expected completed run, got %q (error=%q)", runs[0].Status, runs[0].Error)
	}
	if runs[0].SessionID == "" || runs[0].FinishedAt == nil {
		t.Fatalf("run record incomplete: %+v", runs[0])
	}
}

func TestService_Fire_AgentFailure_RecordsFailedRun(t *testing.T) {
	sink := &eventSink{}
	memStore := storage.NewMemoryStorage()
	svc, err := NewService(Options{
		Store:      memStore,
		Runs:       memStore,
		OnRunEvent: sink.Notify,
		ExecCommand: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		},
		BinaryPath: "/usr/bin/false",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	job := &domain.ScheduledJob{
		ID:             "agent-fails",
		CronExpression: "@every 1s",
		Prompt:         "x",
		CreatedAt:      time.Now().UTC(),
	}
	if err := memStore.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if done, _ := sink.TerminalErr(); done {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	done, termErr := sink.TerminalErr()
	if !done || termErr == nil {
		t.Fatalf("expected failing terminal event, done=%v err=%v", done, termErr)
	}
	runs, err := memStore.ListRuns(context.Background(), "agent-fails")
	if err != nil || len(runs) == 0 {
		t.Fatalf("expected run record, err=%v", err)
	}
	if runs[0].Status != domain.RunStatusFailed || runs[0].Error == "" {
		t.Fatalf("expected failed run with error, got %+v", runs[0])
	}
	loaded, err := memStore.LoadJob(context.Background(), "agent-fails")
	if err != nil {
		t.Fatalf("LoadJob: %v", err)
	}
	if loaded.LastError == "" {
		t.Fatal("expected LastError to be set when the agent run fails")
	}
}

func TestService_PollReload_AddsNewJob(t *testing.T) {
	sink := &eventSink{}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, sink, fired)

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
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	// Wait for the poller to pick up the change (2s poll interval)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(svc.JobIDs()) == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := svc.JobIDs(); len(got) != 1 || got[0] != "added-later" {
		t.Fatalf("expected [added-later] after save, got %v", got)
	}
}

func TestService_PollReload_UpdatesChangedJob(t *testing.T) {
	sink := &eventSink{}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, sink, fired)

	job := &domain.ScheduledJob{
		ID:             "editable",
		CronExpression: "0 8 * * *",
		Prompt:         "before",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	job.Prompt = "after"
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob update: %v", err)
	}

	// The changed fingerprint re-registers the job; there must never be a
	// duplicate or a dropped entry.
	time.Sleep(3 * time.Second)
	if got := svc.JobIDs(); len(got) != 1 || got[0] != "editable" {
		t.Fatalf("expected [editable] after update, got %v", got)
	}
}

func TestService_PollReload_RemovesJob(t *testing.T) {
	sink := &eventSink{}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, sink, fired)

	job := &domain.ScheduledJob{
		ID:             "doomed",
		CronExpression: "0 8 * * *",
		Prompt:         "x",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	if got := len(svc.JobIDs()); got != 1 {
		t.Fatalf("expected 1 job, got %d", got)
	}

	if err := store.DeleteJob(context.Background(), "doomed"); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(svc.JobIDs()) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := svc.JobIDs(); len(got) != 0 {
		t.Fatalf("expected 0 jobs after delete, got %v", got)
	}
}

func TestService_Fire_RunOnce_DeletesAfterFire(t *testing.T) {
	sink := &eventSink{}
	fired := &atomic.Int32{}
	svc, store := newTestService(t, sink, fired)

	job := &domain.ScheduledJob{
		ID:             "one-shot",
		CronExpression: "@every 1s",
		Prompt:         "x",
		RunOnce:        true,
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = svc.Stop(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := store.LoadJob(context.Background(), "one-shot"); errors.Is(err, storage.ErrJobNotFound) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := store.LoadJob(context.Background(), "one-shot"); !errors.Is(err, storage.ErrJobNotFound) {
		t.Fatalf("expected one-off job to be deleted after fire, got err=%v", err)
	}
	if fired.Load() < 1 {
		t.Fatal("expected job to fire at least once")
	}
	// The run record must survive the job deletion.
	runs, err := store.ListRuns(context.Background(), "one-shot")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected run record to survive one-off job deletion")
	}
}
