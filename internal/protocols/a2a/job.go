package a2a

import (
	"context"
	"sync"
	"time"

	baggage "go.opentelemetry.io/otel/baggage"
	trace "go.opentelemetry.io/otel/trace"

	adk "github.com/inference-gateway/adk/types"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// a2aJob adapts a remote A2A task to a BackgroundJob: Run is the polling loop
// (runA2APolling), emitting status updates and returning the terminal result.
// The supervisor owns the goroutine.
type a2aJob struct {
	tool           *SubmitTaskTool
	agentURL       string
	taskID         string
	state          *a2adomain.TaskPollingState
	spanCtx        trace.SpanContext
	bag            baggage.Baggage
	mu             sync.RWMutex
	lastKnownState string
}

// Meta describes the A2A task for the task view.
func (j *a2aJob) Meta() scheddomain.JobMeta {
	return scheddomain.JobMeta{
		ID:           j.taskID,
		Kind:         scheddomain.JobKindA2A,
		Label:        j.taskID,
		Description:  j.state.TaskDescription,
		Detail:       j.agentURL,
		StartedAt:    j.state.StartedAt,
		HoldsSession: true,
	}
}

// Run polls the remote agent until the task terminates. It records each remote
// state change through the emit wrapper so PollingState can report the live
// status to the task view without racing the poll goroutine on the shared state.
//
// The supervisor runs jobs under a fresh context, so the submit span's trace
// context and baggage are re-attached here: every poll then carries the
// session traceparent and the remote agent's spans nest under the submit span
// instead of starting a new trace per poll.
func (j *a2aJob) Run(ctx context.Context, emit func(scheddomain.JobSignal)) agentdomain.ToolExecutionResult {
	ctx = trace.ContextWithSpanContext(ctx, j.spanCtx)
	ctx = baggage.ContextWithBaggage(ctx, j.bag)
	return j.tool.runA2APolling(ctx, j.agentURL, j.taskID, j.state, func(sig scheddomain.JobSignal) {
		j.recordState(sig.State)
		if emit != nil {
			emit(sig)
		}
	})
}

// recordState stores the latest non-empty remote state under mu so PollingState
// can read it without racing the poll goroutine.
func (j *a2aJob) recordState(state string) {
	if state == "" {
		return
	}
	j.mu.Lock()
	j.lastKnownState = state
	j.mu.Unlock()
}

// PollingState reports the task's live polling state, so the supervisor's
// running jobs are the single source for active A2A rows. Identity fields are
// immutable after submit. Only LastKnownState is read under mu, since the poll
// goroutine writes it.
func (j *a2aJob) PollingState() a2adomain.TaskPollingState {
	j.mu.RLock()
	last := j.lastKnownState
	j.mu.RUnlock()

	st := a2adomain.TaskPollingState{
		TaskID:         j.taskID,
		AgentURL:       j.agentURL,
		LastKnownState: last,
		IsPolling:      true,
	}
	if j.state != nil {
		st.ContextID = j.state.ContextID
		st.TaskDescription = j.state.TaskDescription
		st.StartedAt = j.state.StartedAt
	}
	return st
}

// Wind is a no-op: the supervisor cancels Run's context on WindStop, which stops
// the local polling loop. The remote task is cancelled by CancelBackgroundTask
// (the task-view cancel action), whose terminal state the loop then observes.
func (j *a2aJob) Wind(_ context.Context, _ scheddomain.WindSignal) error { return nil }

// Close stops polling on reap (idempotent with the defer in runA2APolling). The
// task stays in the A2A context graph for resume/history.
func (j *a2aJob) Close() {
	if j.tool.taskTracker != nil {
		j.tool.taskTracker.StopPolling(j.taskID)
	}
}

// Finished implements scheddomain.JobFinisher: a completed, failed or canceled
// task stays in the task view, which reads completed A2A rows only from the
// retention service.
func (j *a2aJob) Finished(result agentdomain.ToolExecutionResult) {
	if j.tool.retention == nil {
		return
	}
	if info, ok := j.retainedTask(result); ok {
		j.tool.retention.AddTask(info)
	}
}

// retainedTask builds the TaskInfo to retain for a terminal result.
// result.Data is the live in-memory SubmitTaskResult, never a JSON round-trip.
// Completed and failed carry the full *adk.Task. Canceled does not, so a
// minimal task is rebuilt from the polling state. input-required and any
// other non-terminal state opt out.
func (j *a2aJob) retainedTask(result agentdomain.ToolExecutionResult) (a2adomain.TaskInfo, bool) {
	submit, ok := result.Data.(SubmitTaskResult)
	if !ok || submit.TaskID == "" {
		return a2adomain.TaskInfo{}, false
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
		return a2adomain.TaskInfo{}, false
	}

	return a2adomain.TaskInfo{
		Task:        task,
		AgentURL:    submit.AgentURL,
		StartedAt:   j.state.StartedAt,
		CompletedAt: time.Now(),
	}, true
}

// retainableA2AState reports whether a terminal A2A task state should be kept in
// the task view. input-required is a pause, not a terminal outcome, so it (and
// anything non-terminal) is not retained.
func retainableA2AState(state adk.TaskState) bool {
	switch a2adomain.NormalizeTaskState(state) {
	case adk.TaskStateCompleted, adk.TaskStateFailed, adk.TaskStateCancelled:
		return true
	default:
		return false
	}
}
