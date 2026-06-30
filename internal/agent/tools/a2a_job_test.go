package tools

import (
	"testing"
	"time"

	adk "github.com/inference-gateway/adk/types"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	jobs "github.com/inference-gateway/cli/internal/services/jobs"
	utils "github.com/inference-gateway/cli/internal/utils"
	adkmocks "github.com/inference-gateway/cli/tests/mocks/adk"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// TestA2AJob_PollsRemoteTaskToCompletion drives the migrated A2A path end-to-end:
// the supervisor runs a2aJob.Run (the folded polling loop), which queries the
// remote agent, sees a completed task, returns the result, and stops polling -
// and the supervisor enqueues exactly one completion notification.
func TestA2AJob_PollsRemoteTaskToCompletion(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools:   config.A2AToolsConfig{SubmitTask: config.SubmitTaskToolConfig{Enabled: true}},
			Task:    config.A2ATaskConfig{StatusPollSeconds: 1},
		},
	}

	tracker := utils.NewA2ATaskTracker()
	queue := &mocks.FakeMessageQueue{}
	sup := jobs.NewSupervisor(queue, &mocks.FakeConversationRepository{}, nil)
	defer sup.Stop()

	completed := adk.Task{ID: "t1", Status: adk.TaskStatus{State: adk.TaskStateCompleted}}
	mockClient := &adkmocks.FakeA2AClient{}
	mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: completed}, nil)

	tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, sup, mockClient)
	state := &domain.TaskPollingState{TaskID: "t1", AgentURL: "http://agent", StartedAt: time.Now()}
	tracker.StartPolling("t1", state)
	sup.Submit(&a2aJob{tool: tool, agentURL: "http://agent", taskID: "t1", state: state})

	deadline := time.Now().Add(5 * time.Second)
	for queue.EnqueueCallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("Enqueue called %d times, want 1", n)
	}
	if tracker.IsPolling("t1") {
		t.Fatalf("StopPolling was not called when the task completed")
	}
}

// TestA2AJob_RetainedTask covers the domain.TaskRetainer the supervisor calls on
// finish: completed/failed carry the full *adk.Task; canceled does not, so the task
// is reconstructed from the polling state and reported state; input-required and
// non-A2A results opt out. The asserted fields (state, ids, agent URL, started-at)
// are exactly what the task view reads off a retained TaskInfo.
func TestA2AJob_RetainedTask(t *testing.T) {
	started := time.Now().Add(-2 * time.Minute)
	fullTask := &adk.Task{ID: "t1", ContextID: "ctx1", Status: adk.TaskStatus{State: adk.TaskStateCompleted}}

	tests := []struct {
		name      string
		data      any
		wantOK    bool
		wantState adk.TaskState
		wantID    string
		wantCtx   string
		wantURL   string
	}{
		{
			name:      "completed carries the full task",
			data:      A2ASubmitTaskResult{TaskID: "t1", ContextID: "ctx1", AgentURL: "http://agent", State: string(adk.TaskStateCompleted), Task: fullTask},
			wantOK:    true,
			wantState: adk.TaskStateCompleted,
			wantID:    "t1", wantCtx: "ctx1", wantURL: "http://agent",
		},
		{
			name:      "failed carries the full task",
			data:      A2ASubmitTaskResult{TaskID: "t2", ContextID: "ctx2", AgentURL: "http://agent", State: string(adk.TaskStateFailed), Task: &adk.Task{ID: "t2", ContextID: "ctx2", Status: adk.TaskStatus{State: adk.TaskStateFailed}}},
			wantOK:    true,
			wantState: adk.TaskStateFailed,
			wantID:    "t2", wantCtx: "ctx2", wantURL: "http://agent",
		},
		{
			name:      "canceled without task is reconstructed from state",
			data:      A2ASubmitTaskResult{TaskID: "t3", ContextID: "ctx3", AgentURL: "http://agent", State: string(adk.TaskStateCancelled)},
			wantOK:    true,
			wantState: adk.TaskStateCancelled,
			wantID:    "t3", wantCtx: "ctx3", wantURL: "http://agent",
		},
		{
			name:   "input-required is not retained",
			data:   A2ASubmitTaskResult{TaskID: "t4", AgentURL: "http://agent", State: string(adk.TaskStateInputRequired)},
			wantOK: false,
		},
		{
			name:   "non-submit data is not retained",
			data:   "unrelated",
			wantOK: false,
		},
		{
			name:   "empty task id is not retained",
			data:   A2ASubmitTaskResult{TaskID: "", State: string(adk.TaskStateCompleted)},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &a2aJob{state: &domain.TaskPollingState{StartedAt: started}}
			info, ok := j.RetainedTask(domain.ToolExecutionResult{Data: tt.data})
			if ok != tt.wantOK {
				t.Fatalf("RetainedTask ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if info.Task.Status.State != tt.wantState {
				t.Errorf("state = %q, want %q", info.Task.Status.State, tt.wantState)
			}
			if info.Task.ID != tt.wantID {
				t.Errorf("task id = %q, want %q", info.Task.ID, tt.wantID)
			}
			if info.Task.ContextID != tt.wantCtx {
				t.Errorf("context id = %q, want %q", info.Task.ContextID, tt.wantCtx)
			}
			if info.AgentURL != tt.wantURL {
				t.Errorf("agent url = %q, want %q", info.AgentURL, tt.wantURL)
			}
			if !info.StartedAt.Equal(started) {
				t.Errorf("started at = %v, want %v", info.StartedAt, started)
			}
			if info.CompletedAt.IsZero() {
				t.Error("completed at should be set")
			}
		})
	}
}

// TestRetainableA2AState exercises the state matcher against both the prefixed adk
// enum (TASK_STATE_*) and bare/alternate spellings a remote might report.
func TestRetainableA2AState(t *testing.T) {
	tests := []struct {
		state adk.TaskState
		want  bool
	}{
		{adk.TaskStateCompleted, true},
		{adk.TaskStateFailed, true},
		{adk.TaskStateCancelled, true},
		{"completed", true},
		{"canceled", true},
		{"CANCELLED", true},
		{adk.TaskStateInputRequired, false},
		{adk.TaskStateWorking, false},
		{adk.TaskStateSubmitted, false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := retainableA2AState(tt.state); got != tt.want {
				t.Errorf("retainableA2AState(%q) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}
