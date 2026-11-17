package scheduler

import (
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
	jobs "github.com/inference-gateway/cli/internal/scheduler/jobs"
)

// backgroundTaskRegistry owns all in-flight background work of an agent session,
// promoting the shell and subagent tracker surfaces and composing them with the
// job supervisor. HasPending is the query the agent consults at the completion
// boundary. The trackers keep their own mutexes, so it adds no locking.
type backgroundTaskRegistry struct {
	scheddomain.ShellTracker
	scheddomain.SubagentTracker
	supervisor *jobs.Supervisor
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
// flight, which tells the agent engine whether the session is safe to close. It
// defers to the supervisor, where each job opts in via JobMeta.HoldsSession.
func (r *backgroundTaskRegistry) HasPending() bool {
	return r.supervisor.HasPending()
}
