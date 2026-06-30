package domain

import (
	"context"
	"time"
)

// JobKind identifies which background-work subsystem produced a job so one
// supervisor, one tracker, and one task view can treat A2A tasks, background
// shells, and subagents uniformly while still reporting per-kind counts.
type JobKind string

const (
	JobKindA2A      JobKind = "a2a"
	JobKindShell    JobKind = "shell"
	JobKindSubagent JobKind = "subagent"
)

// JobStatus is the unified lifecycle state across every background-work kind.
type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

// IsTerminal reports whether the job has finished (completed or failed).
func (s JobStatus) IsTerminal() bool { return s == JobCompleted || s == JobFailed }

// JobMeta is the identity/display snapshot a background job exposes. The
// supervisor reads it once at submit (and surfaces it in the task view); it is
// not on any hot path.
type JobMeta struct {
	ID          string
	Kind        JobKind
	Label       string
	Description string
	Detail      string
	StartedAt   time.Time

	Silent bool

	ExcludeFromPending bool
}

// JobSignal is an intermediate, non-terminal event a running job emits to the
// supervisor (an A2A status change, a subagent that became blocked on a tool
// approval). The terminal outcome is Run's return value, never a signal.
type JobSignal struct {
	// Note is a human-facing line surfaced to the UI and, when Enqueue is set,
	// landed on the message queue so the agent reads it on its next turn.
	Note string
	// Enqueue lands Note on the message queue and wakes the agent loop; otherwise
	// Note is a UI-only status update.
	Enqueue bool
	// State is an optional kind-specific status token (e.g. an A2A task state).
	State string
}

// WindSignal is the one-directional graceful control signal the supervisor pushes
// into a running job. WindWrapUp asks it to start finishing (inject a wind-down
// prompt, SIGTERM, or cancel the remote task); WindStop terminates it now (kill
// pane, SIGKILL, cancel). Graceful shutdown sends WindWrapUp to all jobs, waits a
// grace window, then WindStop. The supervisor also uses WindStop as the teardown
// when it reaps a finished job.
type WindSignal int

const (
	WindWrapUp WindSignal = iota
	WindStop
)

// String renders the signal for logs.
func (w WindSignal) String() string {
	if w == WindStop {
		return "stop"
	}
	return "wrap-up"
}

// BackgroundJob is one unit of monitorable background work. The supervisor owns
// the single goroutine that calls Run plus all the lifecycle around it (events,
// queue notification, tracking, cleanup); a job only has to (a) block until it
// reaches a terminal state and return the outcome, and (b) honour wind-down/stop
// signals. This is the seam that lets A2A tasks, shells, and subagents share one
// fan-in implementation - each kind differs only in how it learns it is done.
type BackgroundJob interface {
	// Meta returns the job's identity/display snapshot.
	Meta() JobMeta
	// Run blocks until the job reaches a terminal state or ctx is cancelled,
	// emitting intermediate JobSignals via emit, and returns the terminal result.
	// It MUST return promptly once ctx is cancelled.
	Run(ctx context.Context, emit func(JobSignal)) ToolExecutionResult
	// Wind delivers a graceful wind-down (WindWrapUp) or hard stop (WindStop) to a
	// RUNNING job. It must be safe to call from another goroutine while Run is in
	// flight, be idempotent, and a no-op once the job has finished.
	Wind(ctx context.Context, sig WindSignal) error
	// Close tears down the resources the job owns (its per-kind tracker record, an
	// interactive subagent's tmux pane and temp files, ...). The supervisor calls
	// it exactly once when it reaps a finished job after the retention window. It
	// must be idempotent and must not touch the running process (use Wind for that).
	Close()
}

// JobSubmitter hands a background job to the supervisor. It is the narrow
// projection of BackgroundTaskRegistry that tools use to submit work without
// depending on the whole registry surface.
type JobSubmitter interface {
	Submit(job BackgroundJob)
}

// JobNotifier is an optional BackgroundJob extension. A job that implements it
// formats its own completion-notification body (the text enqueued for the agent
// to read when it finishes) - e.g. a shell reports its exit code and duration,
// which a generic tool-result formatter would not. Jobs that do not implement it
// get the default domain formatting of their ToolExecutionResult.
type JobNotifier interface {
	Notification(result ToolExecutionResult) string
}

// TrackedJob is a point-in-time snapshot of one supervised job for the task view
// and status line.
type TrackedJob struct {
	Meta        JobMeta
	Status      JobStatus
	CompletedAt *time.Time
	LastNote    string
}

// BackgroundJobPhase tags a BackgroundJobEvent's point in a job's lifecycle.
type BackgroundJobPhase string

const (
	JobPhaseSubmitted BackgroundJobPhase = "submitted"
	JobPhaseStatus    BackgroundJobPhase = "status"
	JobPhaseCompleted BackgroundJobPhase = "completed"
	JobPhaseFailed    BackgroundJobPhase = "failed"
)

// BackgroundJobEvent is the single chat event the supervisor emits for every
// background-work kind, replacing the per-kind A2A/Shell/Subagent events. The UI
// renders live progress from it; the authoritative per-job state for the task
// view comes from the tracker snapshot, not from these events.
type BackgroundJobEvent struct {
	RequestID string
	Timestamp time.Time
	Phase     BackgroundJobPhase
	Kind      JobKind
	JobID     string
	Label     string
	Note      string
	Result    *ToolExecutionResult
}

// GetRequestID implements ChatEvent.
func (e BackgroundJobEvent) GetRequestID() string { return e.RequestID }

// GetTimestamp implements ChatEvent.
func (e BackgroundJobEvent) GetTimestamp() time.Time { return e.Timestamp }
