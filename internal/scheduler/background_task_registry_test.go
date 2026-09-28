package scheduler

import (
	"context"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	jobs "github.com/inference-gateway/cli/internal/scheduler/jobs"
)

// fakeMetaJob is a minimal controllable job with caller-supplied meta, standing
// in for any kind in supervisor-sourced liveness tests.
type fakeMetaJob struct {
	meta    scheddomain.JobMeta
	started chan struct{}
	finish  chan struct{}
}

func newFakeMetaJob(meta scheddomain.JobMeta) *fakeMetaJob {
	return &fakeMetaJob{meta: meta, started: make(chan struct{}), finish: make(chan struct{})}
}

func (f *fakeMetaJob) Meta() scheddomain.JobMeta { return f.meta }

func (f *fakeMetaJob) Run(ctx context.Context, _ func(scheddomain.JobSignal)) agentdomain.ToolExecutionResult {
	close(f.started)
	select {
	case <-f.finish:
	case <-ctx.Done():
	}
	return agentdomain.ToolExecutionResult{Success: true}
}

func (f *fakeMetaJob) Wind(context.Context, scheddomain.WindSignal) error { return nil }
func (f *fakeMetaJob) Close()                                             {}

// TestHasPending_ExcludesInteractiveSubagents: a running interactive subagent is
// a live, user-driven tmux pane, so its job declares HoldsSession=false and must
// NOT count as pending background work - otherwise a headless run that opened
// one would hang at exit waiting for it to "finish". A headless subagent holds
// the session and does count.
func TestHasPending_ExcludesInteractiveSubagents(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	reg := NewBackgroundTaskRegistry(4, sup)

	interactive := newFakeMetaJob(scheddomain.JobMeta{ID: "i1", Kind: scheddomain.JobKindSubagent, HoldsSession: false})
	reg.Submit(interactive)
	<-interactive.started
	if reg.HasPending() {
		t.Fatalf("a running interactive subagent must not count as pending")
	}

	headless := newFakeMetaJob(scheddomain.JobMeta{ID: "h1", Kind: scheddomain.JobKindSubagent, HoldsSession: true})
	reg.Submit(headless)
	<-headless.started
	if !reg.HasPending() {
		t.Fatalf("a running headless subagent should count as pending")
	}
	close(headless.finish)
	close(interactive.finish)
}

// TestHasPending_ShellViaSupervisor asserts shell pending-state is read from the
// supervisor.
func TestHasPending_ShellViaSupervisor(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	reg := NewBackgroundTaskRegistry(4, sup)

	if reg.HasPending() {
		t.Fatalf("empty registry must not report pending work")
	}

	shell := newFakeMetaJob(scheddomain.JobMeta{ID: "shell-1", Kind: scheddomain.JobKindShell, HoldsSession: true})
	reg.Submit(shell)
	<-shell.started
	if !reg.HasPending() {
		t.Fatalf("a running supervised shell job should count as pending")
	}
	close(shell.finish)
}
