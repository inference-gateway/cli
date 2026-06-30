package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// fakeJob is a controllable BackgroundJob: Run blocks until finish is closed (or
// ctx is cancelled), emitting any preset signals first, and records Wind calls.
type fakeJob struct {
	meta    domain.JobMeta
	result  domain.ToolExecutionResult
	started chan struct{}
	finish  chan struct{}
	signals []domain.JobSignal

	mu         sync.Mutex
	windCalls  []domain.WindSignal
	closeCalls int
}

func newFakeJob(id string, kind domain.JobKind) *fakeJob {
	return &fakeJob{
		meta:    domain.JobMeta{ID: id, Kind: kind, Label: id, StartedAt: time.Now()},
		result:  domain.ToolExecutionResult{Success: true},
		started: make(chan struct{}),
		finish:  make(chan struct{}),
	}
}

func (f *fakeJob) Meta() domain.JobMeta { return f.meta }

func (f *fakeJob) Run(ctx context.Context, emit func(domain.JobSignal)) domain.ToolExecutionResult {
	close(f.started)
	for _, sig := range f.signals {
		emit(sig)
	}
	select {
	case <-f.finish:
		return f.result
	case <-ctx.Done():
		return domain.ToolExecutionResult{Success: false, Error: "cancelled"}
	}
}

func (f *fakeJob) Wind(_ context.Context, sig domain.WindSignal) error {
	f.mu.Lock()
	f.windCalls = append(f.windCalls, sig)
	f.mu.Unlock()
	return nil
}

func (f *fakeJob) Close() {
	f.mu.Lock()
	f.closeCalls++
	f.mu.Unlock()
}

func (f *fakeJob) winds() []domain.WindSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.WindSignal(nil), f.windCalls...)
}

func (f *fakeJob) closes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCalls
}

func drainEvents(ch <-chan domain.ChatEvent) []domain.BackgroundJobEvent {
	var out []domain.BackgroundJobEvent
	for {
		select {
		case ev := <-ch:
			if bje, ok := ev.(domain.BackgroundJobEvent); ok {
				out = append(out, bje)
			}
		default:
			return out
		}
	}
}

func TestSupervisor_FinishOnce(t *testing.T) {
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{})
	events := make(chan domain.ChatEvent, 32)
	release := sup.BindRequest(events, "req-1", nil)
	defer release()

	job := newFakeJob("job-1", domain.JobKindShell)
	sup.Submit(job)
	<-job.started

	close(job.finish)
	sup.Stop() // waits for the monitor goroutine, so finish has fully run

	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("Enqueue called %d times, want 1", n)
	}
	evs := drainEvents(events)
	var submitted, completed int
	for _, e := range evs {
		switch e.Phase {
		case domain.JobPhaseSubmitted:
			submitted++
		case domain.JobPhaseCompleted:
			completed++
		}
		if e.RequestID != "req-1" {
			t.Fatalf("event RequestID = %q, want req-1", e.RequestID)
		}
	}
	if submitted != 1 || completed != 1 {
		t.Fatalf("events: submitted=%d completed=%d, want 1/1", submitted, completed)
	}

	// The terminal job is kept for the task view until cleanup.
	snap := sup.Snapshot()
	if len(snap) != 1 || snap[0].Status != domain.JobCompleted {
		t.Fatalf("snapshot = %+v, want one completed job", snap)
	}
}

func TestSupervisor_SilentJobDoesNotEnqueue(t *testing.T) {
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{})
	job := newFakeJob("silent", domain.JobKindSubagent)
	job.meta.Silent = true
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	if n := queue.EnqueueCallCount(); n != 0 {
		t.Fatalf("silent job enqueued %d times, want 0", n)
	}
}

// TestSupervisor_PostReleaseDropsEventNoPanic asserts the core safety invariant:
// a job finishing after its request released and CLOSED its event channel drops
// the event instead of panicking on a send to a closed channel.
func TestSupervisor_PostReleaseDropsEventNoPanic(t *testing.T) {
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{})
	events := make(chan domain.ChatEvent, 32)

	release := sup.BindRequest(events, "req-1", nil)
	job := newFakeJob("job-1", domain.JobKindA2A)
	sup.Submit(job)
	<-job.started

	release()
	close(events)

	close(job.finish)
	sup.Stop()

	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("Enqueue called %d times, want 1 (queue is independent of the sink)", n)
	}
}

// TestSupervisor_ReleaseRaceNoPanic exercises release()+close racing a finishing
// job; run under -race it catches an unsynchronised send on a closing channel.
func TestSupervisor_ReleaseRaceNoPanic(t *testing.T) {
	for i := 0; i < 50; i++ {
		sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})
		events := make(chan domain.ChatEvent, 1)
		release := sup.BindRequest(events, "r", nil)
		job := newFakeJob("j", domain.JobKindShell)
		sup.Submit(job)
		<-job.started

		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); close(job.finish) }()
		release()
		close(events)
		wg.Wait()
		sup.Stop()
	}
}

func TestSupervisor_WindStopCancelsRun(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})
	job := newFakeJob("j", domain.JobKindSubagent)
	sup.Submit(job)
	<-job.started

	if err := sup.Wind("j", domain.WindStop); err != nil {
		t.Fatalf("Wind: %v", err)
	}
	sup.Stop() // Run returns because WindStop cancelled its ctx

	winds := job.winds()
	if len(winds) == 0 || winds[0] != domain.WindStop {
		t.Fatalf("wind calls = %v, want [stop]", winds)
	}
	snap := sup.Snapshot()
	if len(snap) != 1 || snap[0].Status != domain.JobFailed {
		t.Fatalf("snapshot = %+v, want one failed (cancelled) job", snap)
	}
}

func TestSupervisor_WindUnknownJob(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})
	if err := sup.Wind("missing", domain.WindWrapUp); err == nil {
		t.Fatalf("expected error winding unknown job")
	}
}

func TestSupervisor_CleanupReapsFinishedAndTearsDown(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})
	job := newFakeJob("j", domain.JobKindSubagent)
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	if n := sup.Cleanup(time.Hour); n != 0 {
		t.Fatalf("Cleanup(1h) reaped %d, want 0", n)
	}
	if n := sup.Cleanup(0); n != 1 {
		t.Fatalf("Cleanup(0) reaped %d, want 1", n)
	}
	if len(sup.Snapshot()) != 0 {
		t.Fatalf("snapshot not empty after cleanup")
	}
	if job.closes() != 1 {
		t.Fatalf("reap called Close %d times, want 1", job.closes())
	}
}

func TestSupervisor_PendingPredicates(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})

	interactive := newFakeJob("interactive", domain.JobKindSubagent)
	interactive.meta.ExcludeFromPending = true
	sup.Submit(interactive)
	<-interactive.started

	if sup.HasPending() {
		t.Fatalf("HasPending true with only an interactive subagent running")
	}
	if !sup.HasActiveWork() {
		t.Fatalf("HasActiveWork false with an interactive subagent running")
	}

	shell := newFakeJob("shell", domain.JobKindShell)
	sup.Submit(shell)
	<-shell.started
	if !sup.HasPending() {
		t.Fatalf("HasPending false with a shell running")
	}
	if got := sup.CountRunning(domain.JobKindSubagent); got != 1 {
		t.Fatalf("CountRunning(subagent) = %d, want 1", got)
	}
	if got := sup.CountRunning(""); got != 2 {
		t.Fatalf("CountRunning(all) = %d, want 2", got)
	}

	sup.Stop()
}

func TestSupervisor_SubmitAfterStopIgnored(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})
	sup.Stop()
	sup.Submit(newFakeJob("late", domain.JobKindShell))
	if len(sup.Snapshot()) != 0 {
		t.Fatalf("job submitted after Stop was tracked")
	}
}

func TestSupervisor_DuplicateSubmitIgnored(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{})
	job := newFakeJob("dup", domain.JobKindShell)
	sup.Submit(job)
	<-job.started
	sup.Submit(newFakeJob("dup", domain.JobKindShell)) // same ID
	if got := sup.CountRunning(""); got != 1 {
		t.Fatalf("CountRunning = %d, want 1 (duplicate ignored)", got)
	}
	sup.Stop()
}
