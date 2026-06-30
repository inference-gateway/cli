package jobs

import (
	"fmt"
	"sync"
	"testing"
	"time"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// recNotifier is a thread-safe domain.UINotifier that records pushed events by
// type, so tests can assert what the supervisor emitted from its goroutines.
type recNotifier struct {
	mu     sync.Mutex
	events []any
}

func (r *recNotifier) Notify(e any) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *recNotifier) counts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := map[string]int{}
	for _, e := range r.events {
		m[fmt.Sprintf("%T", e)]++
	}
	return m
}

// A non-silent job pushes UI events through the notifier instead of relying on a
// poller: BackgroundTasksChangedEvent on submit and on finish, and a single
// DrainQueueEvent when the completion note lands on the queue.
func TestSupervisor_PushesNotifierEvents(t *testing.T) {
	rec := &recNotifier{}
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{}, rec)

	job := newFakeJob("job-1", domain.JobKindShell)
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop() // waits for the monitor goroutine, so finish has fully run

	c := rec.counts()
	if got := c["domain.DrainQueueEvent"]; got != 1 {
		t.Errorf("DrainQueueEvent pushed %d times, want 1 (enqueue on finish)", got)
	}
	if got := c["domain.BackgroundTasksChangedEvent"]; got != 2 {
		t.Errorf("BackgroundTasksChangedEvent pushed %d times, want 2 (submit + finish)", got)
	}
}

// A signalled job that asks to enqueue its note pushes a DrainQueueEvent for that
// signal. The job is silent so the finish path does not also enqueue, isolating
// the signal-driven push.
func TestSupervisor_SignalEnqueuePushesDrain(t *testing.T) {
	rec := &recNotifier{}
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{}, rec)

	job := newFakeJob("job-sig", domain.JobKindSubagent)
	job.meta.Silent = true
	job.signals = []domain.JobSignal{{Note: "progress", Enqueue: true}}
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	if got := rec.counts()["domain.DrainQueueEvent"]; got != 1 {
		t.Errorf("DrainQueueEvent pushed %d times, want 1 (signal enqueue)", got)
	}
}

// Cleanup warns exactly once for a still-running job that has exceeded
// JobRunningLongThreshold; the warnedLong guard suppresses repeats on later sweeps.
func TestSupervisor_CleanupWarnsLongRunningJobOnce(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	prev := logger.GetGlobalLogger()
	logger.SetGlobalLogger(zap.New(core))
	defer logger.SetGlobalLogger(prev)

	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	job := newFakeJob("slow", domain.JobKindShell)
	job.meta.StartedAt = time.Now().Add(-10 * time.Minute) // exceeds the 5m threshold
	sup.Submit(job)
	<-job.started

	// The job is still running, so it is never reaped; only the long-run warning
	// fires, and only on the first sweep.
	sup.Cleanup(time.Hour)
	sup.Cleanup(time.Hour)

	close(job.finish)
	sup.Stop()

	if warns := logs.FilterMessage("job running long").Len(); warns != 1 {
		t.Fatalf("expected exactly 1 'job running long' warning, got %d", warns)
	}
}
