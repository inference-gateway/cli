package a2a

import (
	"context"
	"testing"
	"time"

	adkmocks "github.com/inference-gateway/cli/tests/mocks/adk"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"

	a2adomain "github.com/inference-gateway/cli/internal/a2a/domain"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	jobs "github.com/inference-gateway/cli/internal/scheduler/jobs"
)

// fakeA2ABgJob is a controllable A2A BackgroundJob: it stays running until finish
// (or ctx) and reports its polling detail like a2aJob, so it is visible to
// GetBackgroundTasks and CountRunning while running.
type fakeA2ABgJob struct {
	id      string
	state   a2adomain.TaskPollingState
	started chan struct{}
	finish  chan struct{}
}

func newFakeA2ABgJob(id string, state a2adomain.TaskPollingState) *fakeA2ABgJob {
	return &fakeA2ABgJob{id: id, state: state, started: make(chan struct{}), finish: make(chan struct{})}
}

func (f *fakeA2ABgJob) Meta() scheddomain.JobMeta {
	return scheddomain.JobMeta{ID: f.id, Kind: scheddomain.JobKindA2A, StartedAt: time.Now(), HoldsSession: true}
}

func (f *fakeA2ABgJob) Run(ctx context.Context, _ func(scheddomain.JobSignal)) agentdomain.ToolExecutionResult {
	close(f.started)
	select {
	case <-f.finish:
	case <-ctx.Done():
	}
	return agentdomain.ToolExecutionResult{Success: true}
}

func (f *fakeA2ABgJob) Wind(context.Context, scheddomain.WindSignal) error { return nil }
func (f *fakeA2ABgJob) Close()                                             {}
func (f *fakeA2ABgJob) PollingState() a2adomain.TaskPollingState           { return f.state }

// fakeA2AController stands in for the job supervisor when testing
// BackgroundTaskService in isolation: it returns canned running jobs and
// records Wind calls.
type fakeA2AController struct {
	running  []scheddomain.BackgroundJob
	windIDs  []string
	windSigs []scheddomain.WindSignal
}

func (f *fakeA2AController) RunningJobs(scheddomain.JobKind) []scheddomain.BackgroundJob {
	return f.running
}

func (f *fakeA2AController) Wind(id string, sig scheddomain.WindSignal) error {
	f.windIDs = append(f.windIDs, id)
	f.windSigs = append(f.windSigs, sig)
	return nil
}

// TestGetBackgroundTasks_SourcedFromSupervisor asserts the /tasks active A2A rows
// come from the job supervisor (the single source shared with the status bar),
// with their context/agent/state detail intact.
func TestGetBackgroundTasks_SourcedFromSupervisor(t *testing.T) {
	ctrl := &fakeA2AController{running: []scheddomain.BackgroundJob{
		newFakeA2ABgJob("t1", a2adomain.TaskPollingState{TaskID: "t1", ContextID: "c1", AgentURL: "http://agent", LastKnownState: "working"}),
	}}
	svc := NewBackgroundTaskService(NewTaskTracker(nil), ctrl)

	got := svc.GetBackgroundTasks()
	if len(got) != 1 {
		t.Fatalf("GetBackgroundTasks len = %d, want 1", len(got))
	}
	if got[0].TaskID != "t1" || got[0].ContextID != "c1" || got[0].AgentURL != "http://agent" || got[0].LastKnownState != "working" {
		t.Errorf("A2A detail not preserved: %+v", got[0])
	}
}

// TestCancelBackgroundTask_WindsSupervisor asserts cancel winds the supervised job
// (so the status bar and active list drop it at once) alongside the remote cancel
// and the tracker context-graph cleanup.
func TestCancelBackgroundTask_WindsSupervisor(t *testing.T) {
	tracker := NewTaskTracker(nil)
	tracker.RegisterContext("http://agent", "c1")
	tracker.StartPolling("t1", &a2adomain.TaskPollingState{TaskID: "t1", ContextID: "c1", AgentURL: "http://agent"})

	ctrl := &fakeA2AController{}
	svc := NewBackgroundTaskService(tracker, ctrl)

	adkClient := &adkmocks.FakeA2AClient{}
	adkClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: adk.Task{ID: "t1", Status: adk.TaskStatus{State: adk.TaskStateWorking}}}, nil)
	adkClient.CancelTaskReturns(&adk.JSONRPCSuccessResponse{}, nil)
	svc.createADKClient = func(string) client.A2AClient { return adkClient }

	if err := svc.CancelBackgroundTask("t1"); err != nil {
		t.Fatalf("CancelBackgroundTask: %v", err)
	}
	if len(ctrl.windIDs) != 1 || ctrl.windIDs[0] != "t1" || ctrl.windSigs[0] != scheddomain.WindStop {
		t.Fatalf("want Wind(t1, WindStop), got ids=%v sigs=%v", ctrl.windIDs, ctrl.windSigs)
	}
	if tracker.HasTask("t1") {
		t.Errorf("cancel should remove the task from the tracker context graph")
	}
}

// TestA2ADivergenceGone guards status divergence: while an A2A task runs, the
// status-bar count (CountRunningJobs) and the /tasks active list
// (GetBackgroundTasks) agree because both derive from the supervisor, and when
// it finishes both drop together.
func TestA2ADivergenceGone(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	svc := NewBackgroundTaskService(NewTaskTracker(sup), sup)

	job := newFakeA2ABgJob("t1", a2adomain.TaskPollingState{TaskID: "t1", ContextID: "c1", AgentURL: "http://agent"})
	sup.Submit(job)
	<-job.started

	if c, l := sup.CountRunning(scheddomain.JobKindA2A), len(svc.GetBackgroundTasks()); c != 1 || l != 1 {
		t.Fatalf("while running: CountRunningJobs=%d, GetBackgroundTasks=%d, want 1 and 1", c, l)
	}

	close(job.finish)
	deadline := time.Now().Add(2 * time.Second)
	for (sup.CountRunning(scheddomain.JobKindA2A) != 0 || len(svc.GetBackgroundTasks()) != 0) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if c, l := sup.CountRunning(scheddomain.JobKindA2A), len(svc.GetBackgroundTasks()); c != 0 || l != 0 {
		t.Fatalf("after finish: CountRunningJobs=%d, GetBackgroundTasks=%d, want 0 and 0", c, l)
	}
}

// fakeShellJob is a running non-A2A job, to show a clear leaves other kinds alone.
type fakeShellJob struct{ *fakeA2ABgJob }

func (f fakeShellJob) Meta() scheddomain.JobMeta {
	meta := f.fakeA2ABgJob.Meta()
	meta.Kind = scheddomain.JobKindShell
	return meta
}

// TestClearAllAgents_DiscardsInFlightA2AJobs: clearing the A2A graph (as /clear
// and conversation switch do) also discards the in-flight supervised A2A jobs,
// while shells keep running - a clear is conversation-scoped, not
// session-scoped.
func TestClearAllAgents_DiscardsInFlightA2AJobs(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	tracker := NewTaskTracker(sup)

	tracker.RegisterContext("http://agent", "c1")
	task := newFakeA2ABgJob("t1", a2adomain.TaskPollingState{TaskID: "t1"})
	sup.Submit(task)
	<-task.started

	shell := fakeShellJob{newFakeA2ABgJob("shell-1", a2adomain.TaskPollingState{})}
	sup.Submit(shell)
	<-shell.started

	tracker.ClearAllAgents()

	if sup.CountRunning(scheddomain.JobKindA2A) != 0 {
		t.Fatalf("clear must discard in-flight A2A jobs")
	}
	if sup.CountRunning(scheddomain.JobKindShell) != 1 {
		t.Fatalf("clear must not touch running shells")
	}
	if tracker.HasContext("c1") {
		t.Fatalf("clear must wipe the A2A context graph")
	}
	close(shell.finish)
}
