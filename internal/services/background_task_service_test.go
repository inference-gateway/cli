package services

import (
	"context"
	"testing"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	domain "github.com/inference-gateway/cli/internal/domain"
	jobs "github.com/inference-gateway/cli/internal/services/jobs"
	utils "github.com/inference-gateway/cli/internal/utils"
	adkmocks "github.com/inference-gateway/cli/tests/mocks/adk"
)

// fakeA2ABgJob is a controllable A2A BackgroundJob: it stays running until finish
// (or ctx) and reports A2A polling detail via domain.A2AStateProvider, so it is
// visible to Supervisor.A2APollingStates / CountRunning while running.
type fakeA2ABgJob struct {
	id      string
	state   domain.TaskPollingState
	started chan struct{}
	finish  chan struct{}
}

func newFakeA2ABgJob(id string, state domain.TaskPollingState) *fakeA2ABgJob {
	return &fakeA2ABgJob{id: id, state: state, started: make(chan struct{}), finish: make(chan struct{})}
}

func (f *fakeA2ABgJob) Meta() domain.JobMeta {
	return domain.JobMeta{ID: f.id, Kind: domain.JobKindA2A, StartedAt: time.Now(), HoldsSession: true}
}

func (f *fakeA2ABgJob) Run(ctx context.Context, _ func(domain.JobSignal)) domain.ToolExecutionResult {
	close(f.started)
	select {
	case <-f.finish:
	case <-ctx.Done():
	}
	return domain.ToolExecutionResult{Success: true}
}

func (f *fakeA2ABgJob) Wind(context.Context, domain.WindSignal) error { return nil }
func (f *fakeA2ABgJob) Close()                                        {}
func (f *fakeA2ABgJob) A2APollingState() domain.TaskPollingState      { return f.state }

// fakeA2AController stands in for the job supervisor when testing
// BackgroundTaskService in isolation: it returns canned polling states and
// records Wind calls.
type fakeA2AController struct {
	states   []domain.TaskPollingState
	windIDs  []string
	windSigs []domain.WindSignal
}

func (f *fakeA2AController) A2APollingStates() []domain.TaskPollingState { return f.states }

func (f *fakeA2AController) Wind(id string, sig domain.WindSignal) error {
	f.windIDs = append(f.windIDs, id)
	f.windSigs = append(f.windSigs, sig)
	return nil
}

// TestGetBackgroundTasks_SourcedFromSupervisor asserts the /tasks active A2A rows
// come from the job supervisor (the single source shared with the status bar),
// with their context/agent/state detail intact.
func TestGetBackgroundTasks_SourcedFromSupervisor(t *testing.T) {
	ctrl := &fakeA2AController{states: []domain.TaskPollingState{
		{TaskID: "t1", ContextID: "c1", AgentURL: "http://agent", LastKnownState: "working"},
	}}
	svc := NewBackgroundTaskService(utils.NewA2ATaskTracker(), ctrl)

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
	tracker := utils.NewA2ATaskTracker()
	tracker.RegisterContext("http://agent", "c1")
	tracker.StartPolling("t1", &domain.TaskPollingState{TaskID: "t1", ContextID: "c1", AgentURL: "http://agent"})

	ctrl := &fakeA2AController{}
	svc := NewBackgroundTaskService(tracker, ctrl)

	adkClient := &adkmocks.FakeA2AClient{}
	adkClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: adk.Task{ID: "t1", Status: adk.TaskStatus{State: adk.TaskStateWorking}}}, nil)
	adkClient.CancelTaskReturns(&adk.JSONRPCSuccessResponse{}, nil)
	svc.createADKClient = func(string) client.A2AClient { return adkClient }

	if err := svc.CancelBackgroundTask("t1"); err != nil {
		t.Fatalf("CancelBackgroundTask: %v", err)
	}
	if len(ctrl.windIDs) != 1 || ctrl.windIDs[0] != "t1" || ctrl.windSigs[0] != domain.WindStop {
		t.Fatalf("want Wind(t1, WindStop), got ids=%v sigs=%v", ctrl.windIDs, ctrl.windSigs)
	}
	if tracker.HasTask("t1") {
		t.Errorf("cancel should remove the task from the tracker context graph")
	}
}

// TestA2ADivergenceGone is the regression guard for #693: while an A2A task runs,
// the status-bar count (CountRunningJobs) and the /tasks active list
// (GetBackgroundTasks) agree because both derive from the supervisor, and when it
// finishes both drop together.
func TestA2ADivergenceGone(t *testing.T) {
	sup := jobs.NewSupervisor(nil, nil, nil)
	defer sup.Stop()
	reg := NewBackgroundTaskRegistry(4, sup)
	svc := NewBackgroundTaskService(reg, sup)

	job := newFakeA2ABgJob("t1", domain.TaskPollingState{TaskID: "t1", ContextID: "c1", AgentURL: "http://agent"})
	sup.Submit(job)
	<-job.started

	if c, l := reg.CountRunningJobs(domain.JobKindA2A), len(svc.GetBackgroundTasks()); c != 1 || l != 1 {
		t.Fatalf("while running: CountRunningJobs=%d, GetBackgroundTasks=%d, want 1 and 1", c, l)
	}

	close(job.finish)
	deadline := time.Now().Add(2 * time.Second)
	for (reg.CountRunningJobs(domain.JobKindA2A) != 0 || len(svc.GetBackgroundTasks()) != 0) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if c, l := reg.CountRunningJobs(domain.JobKindA2A), len(svc.GetBackgroundTasks()); c != 0 || l != 0 {
		t.Fatalf("after finish: CountRunningJobs=%d, GetBackgroundTasks=%d, want 0 and 0", c, l)
	}
}
