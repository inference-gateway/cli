package jobs

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	domain "github.com/inference-gateway/cli/internal/domain"
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

// TestSupervisor_FinishOnce: a finished non-silent job lands exactly one note on
// the shared queue (the chat-UI ticker / headless waiter deliver it) and the
// terminal entry is kept for the task view until cleanup. The supervisor has no
// per-request binding - it only ever produces queue messages.
func TestSupervisor_FinishOnce(t *testing.T) {
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{}, nil)

	job := newFakeJob("job-1", domain.JobKindShell)
	sup.Submit(job)
	<-job.started

	close(job.finish)
	sup.Stop()

	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("Enqueue called %d times, want 1", n)
	}

	snap := sup.Snapshot()
	if len(snap) != 1 || snap[0].Status != domain.JobCompleted {
		t.Fatalf("snapshot = %+v, want one completed job", snap)
	}
}

func TestSupervisor_SilentJobDoesNotEnqueue(t *testing.T) {
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{}, nil)
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

// TestSupervisor_ConcurrentFinishEnqueuesEachOnce finishes many jobs at once and
// asserts each lands exactly one queue note. Under -race it guards the
// finish/enqueue path now that there is no per-request sink at all - jobs only
// ever produce queue messages, so a late completion can never send on a closed
// channel.
func TestSupervisor_ConcurrentFinishEnqueuesEachOnce(t *testing.T) {
	queue := &domainmocks.FakeMessageQueue{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{}, nil)

	const n = 16
	js := make([]*fakeJob, n)
	for i := range js {
		js[i] = newFakeJob(fmt.Sprintf("job-%d", i), domain.JobKindShell)
		sup.Submit(js[i])
		<-js[i].started
	}

	var wg sync.WaitGroup
	for _, j := range js {
		wg.Add(1)
		go func(j *fakeJob) { defer wg.Done(); close(j.finish) }(j)
	}
	wg.Wait()
	sup.Stop()

	if got := queue.EnqueueCallCount(); got != n {
		t.Fatalf("Enqueue called %d times, want %d (one per finished job)", got, n)
	}
}

func TestSupervisor_WindStopCancelsRun(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	job := newFakeJob("j", domain.JobKindSubagent)
	sup.Submit(job)
	<-job.started

	if err := sup.Wind("j", domain.WindStop); err != nil {
		t.Fatalf("Wind: %v", err)
	}
	sup.Stop()

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
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	if err := sup.Wind("missing", domain.WindWrapUp); err == nil {
		t.Fatalf("expected error winding unknown job")
	}
}

func TestSupervisor_CleanupReapsFinishedAndTearsDown(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
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

func TestSupervisor_CountRunning(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	sub := newFakeJob("subagent", domain.JobKindSubagent)
	sup.Submit(sub)
	<-sub.started

	shell := newFakeJob("shell", domain.JobKindShell)
	sup.Submit(shell)
	<-shell.started

	if got := sup.CountRunning(domain.JobKindSubagent); got != 1 {
		t.Fatalf("CountRunning(subagent) = %d, want 1", got)
	}
	if got := sup.CountRunning(""); got != 2 {
		t.Fatalf("CountRunning(all) = %d, want 2", got)
	}

	sup.Stop()
}

func TestSupervisor_SubmitAfterStopIgnored(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	sup.Stop()
	sup.Submit(newFakeJob("late", domain.JobKindShell))
	if len(sup.Snapshot()) != 0 {
		t.Fatalf("job submitted after Stop was tracked")
	}
}

func TestSupervisor_DuplicateSubmitIgnored(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	job := newFakeJob("dup", domain.JobKindShell)
	sup.Submit(job)
	<-job.started
	sup.Submit(newFakeJob("dup", domain.JobKindShell)) // same ID
	if got := sup.CountRunning(""); got != 1 {
		t.Fatalf("CountRunning = %d, want 1 (duplicate ignored)", got)
	}
	sup.Stop()
}

// snapByID returns the tracked job with the given ID from the supervisor's
// snapshot, and whether it is present.
func snapByID(sup *Supervisor, id string) (domain.TrackedJob, bool) {
	for _, j := range sup.Snapshot() {
		if j.Meta.ID == id {
			return j, true
		}
	}
	return domain.TrackedJob{}, false
}

func statusOf(sup *Supervisor, id string) domain.JobStatus {
	if j, ok := snapByID(sup, id); ok {
		return j.Status
	}
	return ""
}

func absent(sup *Supervisor, id string) bool {
	_, ok := snapByID(sup, id)
	return !ok
}

// waitFor polls cond until it holds or a short deadline elapses.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

// TestSupervisor_RetentionCountEvictsOldestOnFinish: with a per-kind cap, a
// finishing job that pushes the terminal count over the cap evicts the OLDEST
// terminal job of its kind and tears it down (Close) exactly once. Jobs finish
// sequentially with a gap so completedAt ordering is deterministic.
func TestSupervisor_RetentionCountEvictsOldestOnFinish(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	sup.SetRetentionCount(domain.JobKindShell, 2)

	js := make([]*fakeJob, 3)
	for i := range js {
		js[i] = newFakeJob(fmt.Sprintf("shell-%d", i), domain.JobKindShell)
		sup.Submit(js[i])
		<-js[i].started
	}

	close(js[0].finish)
	waitFor(t, func() bool { return statusOf(sup, "shell-0") == domain.JobCompleted })
	time.Sleep(2 * time.Millisecond)
	close(js[1].finish)
	waitFor(t, func() bool { return statusOf(sup, "shell-1") == domain.JobCompleted })
	time.Sleep(2 * time.Millisecond)
	close(js[2].finish)
	waitFor(t, func() bool { return absent(sup, "shell-0") })

	sup.Stop()

	if !absent(sup, "shell-0") {
		t.Fatalf("oldest shell-0 should have been evicted")
	}
	if js[0].closes() != 1 {
		t.Fatalf("evicted shell-0 Close = %d, want 1", js[0].closes())
	}
	if n := len(sup.Snapshot()); n != 2 {
		t.Fatalf("snapshot len = %d, want 2 (cap)", n)
	}
	for _, id := range []string{"shell-1", "shell-2"} {
		if absent(sup, id) {
			t.Fatalf("%s should remain within the cap", id)
		}
	}
}

// TestSupervisor_RetentionCountPerKindIndependent: caps are tracked per kind, so
// finished shells never evict subagents and vice versa.
func TestSupervisor_RetentionCountPerKindIndependent(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	sup.SetRetentionCount(domain.JobKindShell, 1)
	sup.SetRetentionCount(domain.JobKindSubagent, 1)

	shell0 := newFakeJob("shell-0", domain.JobKindShell)
	sub0 := newFakeJob("sub-0", domain.JobKindSubagent)
	shell1 := newFakeJob("shell-1", domain.JobKindShell)
	sub1 := newFakeJob("sub-1", domain.JobKindSubagent)
	order := []*fakeJob{shell0, sub0, shell1, sub1}
	for _, j := range order {
		sup.Submit(j)
		<-j.started
	}

	for i, j := range order {
		close(j.finish)
		waitFor(t, func() bool {
			return statusOf(sup, j.meta.ID) == domain.JobCompleted || absent(sup, j.meta.ID)
		})
		if i < len(order)-1 {
			time.Sleep(2 * time.Millisecond)
		}
	}
	sup.Stop()

	if n := len(sup.Snapshot()); n != 2 {
		t.Fatalf("snapshot len = %d, want 2 (one per kind)", n)
	}
	for _, id := range []string{"shell-1", "sub-1"} {
		if absent(sup, id) {
			t.Fatalf("newest %s should survive its kind's cap", id)
		}
	}
	for _, id := range []string{"shell-0", "sub-0"} {
		if !absent(sup, id) {
			t.Fatalf("oldest %s should be evicted", id)
		}
	}
	if shell0.closes() != 1 || sub0.closes() != 1 {
		t.Fatalf("evicted jobs Close = shell0:%d sub0:%d, want 1 each", shell0.closes(), sub0.closes())
	}
}

// TestSupervisor_RetentionCountUnsetIsUnbounded: with no cap set, every terminal
// job is retained (current behavior) and none is torn down by retention.
func TestSupervisor_RetentionCountUnsetIsUnbounded(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	js := make([]*fakeJob, 4)
	for i := range js {
		js[i] = newFakeJob(fmt.Sprintf("shell-%d", i), domain.JobKindShell)
		sup.Submit(js[i])
		<-js[i].started
		close(js[i].finish)
	}
	sup.Stop()

	if n := len(sup.Snapshot()); n != 4 {
		t.Fatalf("snapshot len = %d, want 4 (unbounded retention)", n)
	}
	for _, j := range js {
		if j.closes() != 0 {
			t.Fatalf("unbounded retention should not Close jobs, got %d", j.closes())
		}
	}
}

// TestSupervisor_RunningJobNeverEvicted: a live (running) job is never a
// retention victim, so a burst of finished headless subagents cannot reap a live
// interactive subagent pane.
func TestSupervisor_RunningJobNeverEvicted(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	sup.SetRetentionCount(domain.JobKindSubagent, 1)

	live := newFakeJob("interactive", domain.JobKindSubagent)
	sup.Submit(live)
	<-live.started

	h0 := newFakeJob("headless-0", domain.JobKindSubagent)
	sup.Submit(h0)
	<-h0.started
	h1 := newFakeJob("headless-1", domain.JobKindSubagent)
	sup.Submit(h1)
	<-h1.started

	close(h0.finish)
	waitFor(t, func() bool { return statusOf(sup, "headless-0") == domain.JobCompleted })
	time.Sleep(2 * time.Millisecond)
	close(h1.finish)
	waitFor(t, func() bool { return absent(sup, "headless-0") })

	if j, ok := snapByID(sup, "interactive"); !ok || j.Status != domain.JobRunning {
		t.Fatalf("live interactive subagent must stay running, got ok=%v %+v", ok, j)
	}
	if live.closes() != 0 {
		t.Fatalf("running interactive Close = %d, want 0", live.closes())
	}
	if h0.closes() != 1 {
		t.Fatalf("evicted headless-0 Close = %d, want 1", h0.closes())
	}
	if absent(sup, "headless-1") {
		t.Fatalf("newest terminal headless-1 should remain within the cap")
	}

	sup.Stop()
}
