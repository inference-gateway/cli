package services

import (
	domain "github.com/inference-gateway/cli/internal/domain"
	jobs "github.com/inference-gateway/cli/internal/services/jobs"
	utils "github.com/inference-gateway/cli/internal/utils"
)

// backgroundTaskRegistry is the single registry that owns all in-flight
// background work an agent session can produce: A2A tasks AND background
// bash shells. It is the unified replacement for the previously-separate
// A2A task tracker and shell tracker services.
//
// Internally it composes the two existing trackers and exposes their union
// of methods plus an aggregator HasPending() that asks "is *anything*
// happening in the background right now?". This is the single source of
// truth that the BackgroundTasksWaiter consults between turns.
//
// Both embedded trackers retain their own internal mutexes; this struct
// adds no additional locking.
type backgroundTaskRegistry struct {
	*utils.A2ATaskTrackerImpl // promotes the A2ATaskTracker surface
	domain.ShellTracker       // promotes the ShellTracker surface
	domain.SubagentTracker    // promotes the SubagentTracker surface
	supervisor                *jobs.Supervisor
}

// NewBackgroundTaskRegistry constructs the unified registry. maxConcurrentShells
// is the per-session cap enforced by the underlying shell tracker. supervisor is
// the single fan-in that monitors submitted jobs and backs the unified job
// surface (Submit/Snapshot/Wind).
func NewBackgroundTaskRegistry(maxConcurrentShells int, supervisor *jobs.Supervisor) domain.BackgroundTaskRegistry {
	return &backgroundTaskRegistry{
		A2ATaskTrackerImpl: utils.NewA2ATaskTracker(),
		ShellTracker:       utils.NewShellTracker(maxConcurrentShells),
		SubagentTracker:    utils.NewSubagentTracker(),
		supervisor:         supervisor,
	}
}

// Submit delegates to the supervisor.
func (r *backgroundTaskRegistry) Submit(job domain.BackgroundJob) { r.supervisor.Submit(job) }

// Snapshot delegates to the supervisor.
func (r *backgroundTaskRegistry) Snapshot() []domain.TrackedJob { return r.supervisor.Snapshot() }

// CountRunningJobs delegates to the supervisor.
func (r *backgroundTaskRegistry) CountRunningJobs(kind domain.JobKind) int {
	return r.supervisor.CountRunning(kind)
}

// WindJob delegates to the supervisor.
func (r *backgroundTaskRegistry) WindJob(id string, sig domain.WindSignal) error {
	return r.supervisor.Wind(id, sig)
}

// IsJobRunning delegates to the supervisor - the single source of truth for
// whether a supervised job (A2A task, shell, or subagent) is still running.
func (r *backgroundTaskRegistry) IsJobRunning(id string) bool {
	return r.supervisor.IsRunning(id)
}

// ClearAllAgents wipes the A2A context/task graph AND discards the in-flight
// supervised A2A jobs, so a conversation clear/switch cannot leave orphaned
// pollers running (still counted in the status bar, listed in /tasks, and able
// to land a late completion note). Shells and subagents are deliberately
// untouched - they are session-scoped, not conversation-scoped.
func (r *backgroundTaskRegistry) ClearAllAgents() {
	r.supervisor.DiscardKind(domain.JobKindA2A)
	r.A2ATaskTrackerImpl.ClearAllAgents()
}

// HasPending reports whether any session-holding background job is still in
// flight, regardless of kind - the cross-type query the BackgroundTasksWaiter
// uses to decide whether the session is safe to close. It defers to the
// supervisor (the single source of truth); each job opts in via
// JobMeta.HoldsSession, so interactive subagent panes are excluded there rather
// than by a per-kind check here.
func (r *backgroundTaskRegistry) HasPending() bool {
	return r.supervisor.HasPending()
}
