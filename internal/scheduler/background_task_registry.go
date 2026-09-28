package scheduler

import (
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
	jobs "github.com/inference-gateway/cli/internal/scheduler/jobs"
)

// backgroundTaskRegistry is the single registry that owns all in-flight
// background work an agent session can produce. It composes the shell and
// subagent trackers with the job supervisor. HasPending is the "is *anything*
// running?" query the agent engine consults at the completion boundary. The
// embedded trackers keep their own mutexes, so this struct adds no locking.
type backgroundTaskRegistry struct {
	scheddomain.ShellTracker    // promotes the ShellTracker surface
	scheddomain.SubagentTracker // promotes the SubagentTracker surface
	supervisor                  *jobs.Supervisor
}

// NewBackgroundTaskRegistry constructs the unified registry. maxConcurrentShells
// is the per-session cap enforced by the underlying shell tracker. supervisor is
// the single fan-in that monitors submitted jobs and backs the unified job
// surface (Submit/Snapshot/Wind).
func NewBackgroundTaskRegistry(maxConcurrentShells int, supervisor *jobs.Supervisor) scheddomain.BackgroundTaskRegistry {
	return &backgroundTaskRegistry{
		ShellTracker:    schedinfra.NewShellTracker(maxConcurrentShells),
		SubagentTracker: schedinfra.NewSubagentTracker(),
		supervisor:      supervisor,
	}
}

// Submit delegates to the supervisor.
func (r *backgroundTaskRegistry) Submit(job scheddomain.BackgroundJob) { r.supervisor.Submit(job) }

// Snapshot delegates to the supervisor.
func (r *backgroundTaskRegistry) Snapshot() []scheddomain.TrackedJob { return r.supervisor.Snapshot() }

// CountRunningJobs delegates to the supervisor.
func (r *backgroundTaskRegistry) CountRunningJobs(kind scheddomain.JobKind) int {
	return r.supervisor.CountRunning(kind)
}

// WindJob delegates to the supervisor.
func (r *backgroundTaskRegistry) WindJob(id string, sig scheddomain.WindSignal) error {
	return r.supervisor.Wind(id, sig)
}

// IsJobRunning delegates to the supervisor - the single source of truth for
// whether a supervised job is still running.
func (r *backgroundTaskRegistry) IsJobRunning(id string) bool {
	return r.supervisor.IsRunning(id)
}

// HasPending reports whether any session-holding background job is still in
// flight, regardless of kind - the cross-type query the agent engine
// uses to decide whether the session is safe to close. It defers to the
// supervisor (the single source of truth); each job opts in via
// JobMeta.HoldsSession, so interactive subagent panes are excluded there rather
// than by a per-kind check here.
func (r *backgroundTaskRegistry) HasPending() bool {
	return r.supervisor.HasPending()
}
