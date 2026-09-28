package domain

import (
	"context"
)

// BackgroundTaskRegistry is the single tracker that owns *all* in-flight
// background work an agent session can produce: background bash shells and
// subagents, plus every job submitted to the supervisor (such as a task
// delegated to an A2A agent). Callers that need less depend on the narrower
// ShellTracker or SubagentTracker, or on a Job* projection.
type BackgroundTaskRegistry interface {
	ShellTracker
	SubagentTracker

	// HasPending reports whether *any* background work is still in flight,
	// regardless of type. True when there is at least one session-holding job
	// running (an A2A task, a background shell, or a HEADLESS subagent).
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
