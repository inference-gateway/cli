package services

import (
	"context"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	jobs "github.com/inference-gateway/cli/internal/services/jobs"
)

// fakeMetaJob is a minimal controllable job with caller-supplied meta, standing
// in for any kind in supervisor-sourced liveness tests.
type fakeMetaJob struct {
	meta    domain.JobMeta
	started chan struct{}
	finish  chan struct{}
}

func newFakeMetaJob(meta domain.JobMeta) *fakeMetaJob {
	return &fakeMetaJob{meta: meta, started: make(chan struct{}), finish: make(chan struct{})}
}

func (f *fakeMetaJob) Meta() domain.JobMeta { return f.meta }

func (f *fakeMetaJob) Run(ctx context.Context, _ func(domain.JobSignal)) domain.ToolExecutionResult {
	close(f.started)
	select {
	case <-f.finish:
	case <-ctx.Done():
	}
	return domain.ToolExecutionResult{Success: true}
}

func (f *fakeMetaJob) Wind(context.Context, domain.WindSignal) error { return nil }
func (f *fakeMetaJob) Close()                                        {}

// TestHasPending_ExcludesInteractiveSubagents: a running interactive subagent is
// a live, user-driven tmux pane, so its job declares HoldsSession=false and must
// NOT count as pending background work - otherwise a headless run that opened
// one would hang at exit waiting for it to "finish". A headless subagent holds
// the session and does count.
func TestHasPending_ExcludesInteractiveSubagents(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	reg := NewBackgroundTaskRegistry(4, sup)

	interactive := newFakeMetaJob(domain.JobMeta{ID: "i1", Kind: domain.JobKindSubagent, HoldsSession: false})
	reg.Submit(interactive)
	<-interactive.started
	if reg.HasPending() {
		t.Fatalf("a running interactive subagent must not count as pending")
	}

	headless := newFakeMetaJob(domain.JobMeta{ID: "h1", Kind: domain.JobKindSubagent, HoldsSession: true})
	reg.Submit(headless)
	<-headless.started
	if !reg.HasPending() {
		t.Fatalf("a running headless subagent should count as pending")
	}
	close(headless.finish)
	close(interactive.finish)
}

// TestHasPending_A2AViaSupervisor asserts A2A pending-state is read from the job
// supervisor (single source of truth), not the parallel tracker polling set: a
// StartPolling entry with no supervised job is NOT pending, while a submitted
// (running) A2A job IS.
func TestHasPending_A2AViaSupervisor(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	reg := NewBackgroundTaskRegistry(4, sup)

	reg.RegisterContext("http://agent", "c1")
	reg.StartPolling("t1", &domain.TaskPollingState{TaskID: "t1", ContextID: "c1", AgentURL: "http://agent"})
	if reg.HasPending() {
		t.Fatalf("StartPolling without a supervised job must not count as pending")
	}

	job := newFakeA2ABgJob("t1", domain.TaskPollingState{TaskID: "t1"})
	reg.Submit(job)
	<-job.started
	if !reg.HasPending() {
		t.Fatalf("a running supervised A2A job should count as pending")
	}
	close(job.finish)
}

// TestHasPending_ShellViaSupervisor asserts shell pending-state is read from the
// supervisor too, mirroring the A2A path.
func TestHasPending_ShellViaSupervisor(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	reg := NewBackgroundTaskRegistry(4, sup)

	if reg.HasPending() {
		t.Fatalf("empty registry must not report pending work")
	}

	shell := newFakeMetaJob(domain.JobMeta{ID: "shell-1", Kind: domain.JobKindShell, HoldsSession: true})
	reg.Submit(shell)
	<-shell.started
	if !reg.HasPending() {
		t.Fatalf("a running supervised shell job should count as pending")
	}
	close(shell.finish)
}

// TestClearAllAgents_DiscardsInFlightA2AJobs: clearing the A2A graph (as /clear
// and conversation switch do) also discards the in-flight supervised A2A jobs,
// while shells keep running - a clear is conversation-scoped, not
// session-scoped.
func TestClearAllAgents_DiscardsInFlightA2AJobs(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	reg := NewBackgroundTaskRegistry(4, sup)

	reg.RegisterContext("http://agent", "c1")
	task := newFakeA2ABgJob("t1", domain.TaskPollingState{TaskID: "t1"})
	reg.Submit(task)
	<-task.started

	shell := newFakeMetaJob(domain.JobMeta{ID: "shell-1", Kind: domain.JobKindShell, HoldsSession: true})
	reg.Submit(shell)
	<-shell.started

	reg.ClearAllAgents()

	if reg.CountRunningJobs(domain.JobKindA2A) != 0 {
		t.Fatalf("clear must discard in-flight A2A jobs")
	}
	if reg.CountRunningJobs(domain.JobKindShell) != 1 {
		t.Fatalf("clear must not touch running shells")
	}
	if reg.HasContext("c1") {
		t.Fatalf("clear must wipe the A2A context graph")
	}
	close(shell.finish)
}
