package tools

import (
	"context"
	"strings"
	"time"

	adk "github.com/inference-gateway/adk/types"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// a2aJob adapts a remote A2A task to a BackgroundJob: Run is the polling loop
// (runA2APolling, formerly pollTaskInBackground), emitting status updates and
// returning the terminal result. The supervisor owns the goroutine - this is the
// A2A side of retiring A2ATaskPoller.
type a2aJob struct {
	tool     *A2ASubmitTaskTool
	agentURL string
	taskID   string
	state    *domain.TaskPollingState
}

// Meta describes the A2A task for the task view.
func (j *a2aJob) Meta() domain.JobMeta {
	return domain.JobMeta{
		ID:          j.taskID,
		Kind:        domain.JobKindA2A,
		Label:       j.taskID,
		Description: j.state.TaskDescription,
		Detail:      j.agentURL,
		StartedAt:   j.state.StartedAt,
	}
}

// Run polls the remote agent until the task terminates.
func (j *a2aJob) Run(ctx context.Context, emit func(domain.JobSignal)) domain.ToolExecutionResult {
	return j.tool.runA2APolling(ctx, j.agentURL, j.taskID, j.state, emit)
}

// Wind is a no-op: the supervisor cancels Run's context on WindStop, which stops
// the local polling loop. The remote task is cancelled by CancelBackgroundTask
// (the task-view cancel action), whose terminal state the loop then observes.
func (j *a2aJob) Wind(_ context.Context, _ domain.WindSignal) error { return nil }

// Close stops polling on reap (idempotent with the defer in runA2APolling). The
// task stays in the A2A context graph for resume/history.
func (j *a2aJob) Close() {
	if j.tool.taskTracker != nil {
		j.tool.taskTracker.StopPolling(j.taskID)
	}
}

// RetainedTask implements domain.TaskRetainer: when the polling loop returns a
// terminal result, hand the supervisor a TaskInfo so the completed/failed/canceled
// task stays in the task view (which reads completed A2A rows only from the
// retention service). result.Data is the live in-memory A2ASubmitTaskResult, so the
// type assertion is safe here (no JSON round-trip). Completed/failed carry the full
// *adk.Task; canceled does not, so reconstruct a minimal task from the polling state
// and reported state. input-required (and any non-terminal state) opts out.
func (j *a2aJob) RetainedTask(result domain.ToolExecutionResult) (domain.TaskInfo, bool) {
	submit, ok := result.Data.(A2ASubmitTaskResult)
	if !ok || submit.TaskID == "" {
		return domain.TaskInfo{}, false
	}

	task := adk.Task{
		ID:        submit.TaskID,
		ContextID: submit.ContextID,
		Status:    adk.TaskStatus{State: adk.TaskState(submit.State)},
	}
	if submit.Task != nil {
		task = *submit.Task
	}

	if !retainableA2AState(task.Status.State) {
		return domain.TaskInfo{}, false
	}

	return domain.TaskInfo{
		Task:        task,
		AgentURL:    submit.AgentURL,
		StartedAt:   j.state.StartedAt,
		CompletedAt: time.Now(),
	}, true
}

// retainableA2AState reports whether a terminal A2A task state should be kept in the
// task view. adk states are prefixed enums (TASK_STATE_COMPLETED, ...) but a remote
// may report bare forms, so normalize the same way handleTaskState does - lowercase
// and drop a "task_state_" prefix - before matching. input-required is a pause, not a
// terminal outcome, so it (and anything non-terminal) is not retained.
func retainableA2AState(state adk.TaskState) bool {
	switch strings.TrimPrefix(strings.ToLower(string(state)), "task_state_") {
	case "completed", "failed", "cancelled", "canceled":
		return true
	default:
		return false
	}
}
