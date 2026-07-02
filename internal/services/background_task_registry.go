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

// HasPending reports whether *any* background work is still in flight,
// regardless of type. True when there is at least one A2A task being polled,
// one running background shell, OR one running local subagent. This is the
// cross-type query the BackgroundTasksWaiter uses to decide whether the session
// is safe to close.
func (r *backgroundTaskRegistry) HasPending() bool {
	if r.supervisor.CountRunning(domain.JobKindA2A) > 0 {
		return true
	}
	if r.ShellTracker != nil && r.CountRunning() > 0 {
		return true
	}
	if r.SubagentTracker != nil && r.countPendingSubagents() > 0 {
		return true
	}
	return false
}

// countPendingSubagents counts only headless running subagents. Interactive
// subagents are live, user-driven tmux panes managed via the subagent tools;
// they must not keep a session open waiting on them, or a headless run that
// opened one would hang at exit until the background-task wait times out.
func (r *backgroundTaskRegistry) countPendingSubagents() int {
	n := 0
	for _, s := range r.GetAllSubagents() {
		if s.Status == domain.SubagentRunning && s.Mode != domain.SubagentModeInteractive {
			n++
		}
	}
	return n
}
