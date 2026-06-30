package tools

import (
	"context"

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
