// Background task tracking and retention contracts of the scheduler context.

package domain

import (
	"context"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	"time"

	adk "github.com/inference-gateway/adk/types"
)

// TaskInfo wraps ADK Task with UI-specific metadata for completed/terminal tasks
// Used for A2A task retention and display
type TaskInfo struct {
	// ADK Task contains: ID, ContextID, Status (with State), History, Artifacts, Metadata
	Task adk.Task

	// UI-specific fields
	AgentURL    string
	StartedAt   time.Time
	CompletedAt time.Time
}

// TaskRetentionService manages in-memory retention of completed/terminal A2A tasks
// Only enabled when A2A is enabled - decouples task retention from StateManager
type TaskRetentionService interface {
	// AddTask adds a terminal task (completed, failed, canceled, etc.) to retention
	AddTask(task TaskInfo)

	// GetTasks returns all retained tasks
	GetTasks() []TaskInfo

	// Clear removes all retained tasks
	Clear()

	// SetMaxRetention updates the maximum retention count
	SetMaxRetention(maxRetention int)

	// GetMaxRetention returns the current maximum retention count
	GetMaxRetention() int
}

// BackgroundTaskService handles background A2A task operations
// Only enabled when A2A is enabled - provides task cancellation and retrieval
type BackgroundTaskService interface {
	// GetBackgroundTasks returns all current background polling tasks
	GetBackgroundTasks() []agentdomain.TaskPollingState

	// CancelBackgroundTask cancels a background task by task ID
	CancelBackgroundTask(taskID string) error
}

// A2AClearer is the one-method projection of the A2A tracker used by
// conversation clear/switch to discard the A2A context/task graph. The concrete
// *utils.A2ATaskTrackerImpl (and the BackgroundTaskRegistry that embeds it)
// satisfies it; consumers that only clear depend on this instead of the whole
// tracker.
type A2AClearer interface {
	ClearAllAgents()
}

// BackgroundTaskRegistry is the single tracker that owns *all* in-flight
// background work an agent session can produce: A2A tasks (long-running
// work delegated to remote agents) and background bash shells (long-running
// commands the agent has detached from the foreground). Both are
// conceptually the same thing - async producers of results that need to
// land back on the conversation when they finish - so they live behind one
// type here.
//
// The interface unifies what used to be two separate trackers
// (A2ATaskTracker and ShellTracker) via composition: depending on what a
// caller needs, it can use the narrower A2ATaskTracker or ShellTracker
// interface, or this full BackgroundTaskRegistry to access both plus the
// HasPending() aggregator method.
type BackgroundTaskRegistry interface {
	agentdomain.A2ATaskTracker
	ShellTracker
	SubagentTracker

	// HasPending reports whether *any* background work is still in flight,
	// regardless of type. True when there is at least one A2A task being
	// polled, one running background shell, OR one running HEADLESS subagent.
	// It deliberately excludes interactive subagents so a one-shot `infer headless`
	// does not hang at exit waiting on a user-driven tmux pane.
	HasPending() bool

	// Submit hands a background job to the supervisor, which spawns its monitor
	// goroutine and folds its result back onto the conversation when it finishes.
	// This is the single entry point every kind (A2A task, shell, subagent) uses
	// instead of running its own poller.
	Submit(job BackgroundJob)

	// Snapshot returns the supervisor's view of all live and recently-finished
	// jobs for the task view and status line.
	Snapshot() []TrackedJob

	// CountRunningJobs returns how many supervised jobs are running, optionally
	// filtered to one kind (pass "" for all kinds).
	CountRunningJobs(kind JobKind) int

	// IsJobRunning reports whether the supervised job with the given id is still
	// running. It is the per-id liveness query a tool uses (via the narrow
	// JobLivenessReporter projection) to defer to the supervisor - the single
	// source of truth - instead of racing it with a manual read.
	IsJobRunning(id string) bool

	// WindJob sends a graceful wind-down or hard stop to one supervised job.
	WindJob(id string, sig WindSignal) error
}

// TitleGenerator interface for conversation title generation
type TitleGenerator interface {
	ProcessPendingTitles(ctx context.Context) error
}
