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
	sup := jobs.NewSupervisor(queue, &mocks.FakeConversationRepository{})
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
