package jobs

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	adk "github.com/inference-gateway/adk/types"

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

// fakeRetainerJob is a fakeJob that also implements domain.TaskRetainer, returning a
// preset TaskInfo/ok so the supervisor's retain-on-finish path can be tested in
// isolation from any real job's extraction logic.
type fakeRetainerJob struct {
	*fakeJob
	info domain.TaskInfo
	ok   bool
}

func (j *fakeRetainerJob) RetainedTask(domain.ToolExecutionResult) (domain.TaskInfo, bool) {
	return j.info, j.ok
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

func TestSupervisor_IsRunning(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	job := newFakeJob("j1", domain.JobKindA2A)
	sup.Submit(job)
	<-job.started

	if !sup.IsRunning("j1") {
		t.Fatalf("IsRunning(j1) = false while the job is running")
	}
	if sup.IsRunning("missing") {
		t.Fatalf("IsRunning(missing) = true, want false")
	}

	close(job.finish)
	sup.Stop()

	if sup.IsRunning("j1") {
		t.Fatalf("IsRunning(j1) = true after the job finished")
	}
}

// TestSupervisor_HasPending: only running jobs that hold the session count as
// pending - a running non-session-holding job (an interactive pane) and
// finished jobs do not.
func TestSupervisor_HasPending(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	pane := newFakeJob("pane", domain.JobKindSubagent)
	sup.Submit(pane)
	<-pane.started
	if sup.HasPending() {
		t.Fatalf("a running non-session-holding job must not count as pending")
	}

	holder := newFakeJob("task", domain.JobKindA2A)
	holder.meta.HoldsSession = true
	sup.Submit(holder)
	<-holder.started
	if !sup.HasPending() {
		t.Fatalf("a running session-holding job should count as pending")
	}

	close(holder.finish)
	close(pane.finish)
	sup.Stop()

	if sup.HasPending() {
		t.Fatalf("finished jobs must not count as pending")
	}
}

// TestSupervisor_DiscardKind: discarding a kind stops and forgets its running
// jobs - dropped from the snapshot immediately, hard-stopped, no queue note, no
// retention entry - while other kinds keep running untouched.
func TestSupervisor_DiscardKind(t *testing.T) {
	queue := &domainmocks.FakeMessageQueue{}
	retention := &domainmocks.FakeTaskRetentionService{}
	sup := NewSupervisor(queue, &domainmocks.FakeConversationRepository{}, nil)
	sup.SetTaskRetention(retention)

	a2a := &fakeRetainerJob{fakeJob: newFakeJob("t1", domain.JobKindA2A), info: domain.TaskInfo{}, ok: true}
	sup.Submit(a2a)
	<-a2a.started

	shell := newFakeJob("shell-1", domain.JobKindShell)
	sup.Submit(shell)
	<-shell.started

	sup.DiscardKind(domain.JobKindA2A)

	if sup.CountRunning(domain.JobKindA2A) != 0 {
		t.Fatalf("discarded A2A job still counted as running")
	}
	if sup.CountRunning(domain.JobKindShell) != 1 {
		t.Fatalf("shell must keep running through an A2A discard")
	}
	for _, tj := range sup.Snapshot() {
		if tj.Meta.ID == "t1" {
			t.Fatalf("discarded job still in snapshot: %+v", tj)
		}
	}
	if winds := a2a.winds(); len(winds) != 1 || winds[0] != domain.WindStop {
		t.Fatalf("discard winds = %v, want [stop]", winds)
	}

	close(shell.finish)
	sup.Stop()

	if n := retention.AddTaskCallCount(); n != 0 {
		t.Fatalf("discarded job landed %d retention entries, want 0", n)
	}
	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("Enqueue called %d times, want 1 (shell only - a discarded job must not note)", n)
	}
	if n := a2a.closes(); n != 1 {
		t.Fatalf("discarded job Close called %d times, want 1", n)
	}
}

// TestSupervisor_DiscardKind_ReapsTerminal: an already-finished (unreaped) job
// of the kind is dropped and torn down by the discard sweep.
func TestSupervisor_DiscardKind_ReapsTerminal(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	job := newFakeJob("t1", domain.JobKindA2A)
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	if len(sup.Snapshot()) != 1 {
		t.Fatalf("terminal job should still be tracked before discard")
	}
	sup.DiscardKind(domain.JobKindA2A)
	if len(sup.Snapshot()) != 0 {
		t.Fatalf("terminal job should be reaped by discard")
	}
	if n := job.closes(); n != 1 {
		t.Fatalf("Close called %d times, want 1", n)
	}
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

// TestSupervisor_FinishRetainsTerminalTask: a finished job implementing TaskRetainer
// (opting in) has its TaskInfo handed to the retention service exactly once, so the
// completed A2A task stays in the task view after its monitor goroutine exits.
func TestSupervisor_FinishRetainsTerminalTask(t *testing.T) {
	retention := &domainmocks.FakeTaskRetentionService{}
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	sup.SetTaskRetention(retention)

	info := domain.TaskInfo{AgentURL: "http://agent", Task: adk.Task{ID: "t1", Status: adk.TaskStatus{State: adk.TaskStateCompleted}}}
	job := &fakeRetainerJob{fakeJob: newFakeJob("t1", domain.JobKindA2A), info: info, ok: true}
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	if n := retention.AddTaskCallCount(); n != 1 {
		t.Fatalf("AddTask called %d times, want 1", n)
	}
	if got := retention.AddTaskArgsForCall(0); got.AgentURL != "http://agent" || got.Task.ID != "t1" {
		t.Fatalf("AddTask got %+v, want AgentURL=http://agent Task.ID=t1", got)
	}
}

// TestSupervisor_FinishSkipsRetention: retention is untouched when the job is not a
// TaskRetainer, or is one that opts out (ok=false).
func TestSupervisor_FinishSkipsRetention(t *testing.T) {
	t.Run("not a retainer", func(t *testing.T) {
		retention := &domainmocks.FakeTaskRetentionService{}
		sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
		sup.SetTaskRetention(retention)

		job := newFakeJob("plain", domain.JobKindShell)
		sup.Submit(job)
		<-job.started
		close(job.finish)
		sup.Stop()

		if n := retention.AddTaskCallCount(); n != 0 {
			t.Fatalf("AddTask called %d times, want 0", n)
		}
	})

	t.Run("retainer opts out", func(t *testing.T) {
		retention := &domainmocks.FakeTaskRetentionService{}
		sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
		sup.SetTaskRetention(retention)

		job := &fakeRetainerJob{fakeJob: newFakeJob("optout", domain.JobKindA2A), ok: false}
		sup.Submit(job)
		<-job.started
		close(job.finish)
		sup.Stop()

		if n := retention.AddTaskCallCount(); n != 0 {
			t.Fatalf("AddTask called %d times, want 0", n)
		}
	})
}

// TestSupervisor_FinishWithoutRetentionService: a retainer job finishing with no
// retention service wired must not panic, and the job still completes normally.
func TestSupervisor_FinishWithoutRetentionService(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	job := &fakeRetainerJob{fakeJob: newFakeJob("t1", domain.JobKindA2A), info: domain.TaskInfo{AgentURL: "http://agent"}, ok: true}
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	if j, ok := snapByID(sup, "t1"); !ok || j.Status != domain.JobCompleted {
		t.Fatalf("job should complete normally, got ok=%v %+v", ok, j)
	}
}

// fakeOutputJob is a fakeJob that also implements domain.JobOutputProvider, returning
// a preset output string so the supervisor's snapshot-output path can be tested in
// isolation from any real job's output extraction logic.
type fakeOutputJob struct {
	*fakeJob
	output string
}

func (j *fakeOutputJob) Output() string { return j.output }

// TestSupervisor_SnapshotPopulatesOutput: a job implementing JobOutputProvider
// has its output populated in the snapshot. This is the core data path for the
// /tasks detail panel "Output" section for shells and subagents.
func TestSupervisor_SnapshotPopulatesOutput(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	job := &fakeOutputJob{
		fakeJob: newFakeJob("output-shell", domain.JobKindShell),
		output:  "hello from background shell\nline2\nline3",
	}
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	snap := sup.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if snap[0].Output != job.output {
		t.Fatalf("snapshot Output = %q, want %q", snap[0].Output, job.output)
	}
}

// TestSupervisor_SnapshotOmitsOutputForNonProvider: a job that does not
// implement JobOutputProvider has an empty Output in the snapshot.
func TestSupervisor_SnapshotOmitsOutputForNonProvider(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	job := newFakeJob("no-output", domain.JobKindShell)
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	snap := sup.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if snap[0].Output != "" {
		t.Fatalf("snapshot Output = %q, want empty string for non-OutputProvider job", snap[0].Output)
	}
}

// TestSupervisor_SnapshotPopulatesOutputAfterCleanupReap: output survives in the
// snapshot until the job is reaped by cleanup, not cleared on finish.
func TestSupervisor_SnapshotPopulatesOutputAfterCleanupReap(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)

	job := &fakeOutputJob{
		fakeJob: newFakeJob("output-reap", domain.JobKindShell),
		output:  "output that must survive until cleanup",
	}
	sup.Submit(job)
	<-job.started
	close(job.finish)
	sup.Stop()

	snap := sup.Snapshot()
	if len(snap) != 1 || snap[0].Output != job.output {
		t.Fatalf("output lost before cleanup: got %+v", snap)
	}

	sup.Cleanup(0)
	if len(sup.Snapshot()) != 0 {
		t.Fatalf("snapshot not empty after cleanup")
	}
}

// fakeA2AJob is a fakeJob that also implements domain.A2AStateProvider, so it is
// visible to Supervisor.A2APollingStates while running.
type fakeA2AJob struct {
	*fakeJob
	state domain.TaskPollingState
}

func (f *fakeA2AJob) A2APollingState() domain.TaskPollingState { return f.state }

// TestSupervisor_A2APollingStates asserts the supervisor is the single source for
// active A2A rows: only running A2A jobs are returned, with their polling detail
// intact, and a finished task drops out.
func TestSupervisor_A2APollingStates(t *testing.T) {
	sup := NewSupervisor(&domainmocks.FakeMessageQueue{}, &domainmocks.FakeConversationRepository{}, nil)
	defer sup.Stop()

	a2a := &fakeA2AJob{
		fakeJob: newFakeJob("a1", domain.JobKindA2A),
		state:   domain.TaskPollingState{TaskID: "a1", ContextID: "ctx1", AgentURL: "http://agent", LastKnownState: "working"},
	}
	shell := newFakeJob("s1", domain.JobKindShell)

	sup.Submit(a2a)
	sup.Submit(shell)
	<-a2a.started
	<-shell.started

	states := sup.A2APollingStates()
	if len(states) != 1 {
		t.Fatalf("A2APollingStates len = %d, want 1 (A2A only, excludes shell)", len(states))
	}
	if got := states[0]; got.TaskID != "a1" || got.ContextID != "ctx1" || got.AgentURL != "http://agent" || got.LastKnownState != "working" {
		t.Errorf("A2A detail not preserved: %+v", got)
	}

	close(a2a.finish)
	waitFor(t, func() bool { return len(sup.A2APollingStates()) == 0 })
}
