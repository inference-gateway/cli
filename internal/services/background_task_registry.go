package services

import (
	domain "github.com/inference-gateway/cli/internal/domain"
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
}

// NewBackgroundTaskRegistry constructs the unified registry. maxConcurrentShells
// is the per-session cap enforced by the underlying shell tracker.
func NewBackgroundTaskRegistry(maxConcurrentShells int) domain.BackgroundTaskRegistry {
	return &backgroundTaskRegistry{
		A2ATaskTrackerImpl: utils.NewA2ATaskTracker(),
		ShellTracker:       utils.NewShellTracker(maxConcurrentShells),
	}
}

// HasPending reports whether *any* background work is still in flight,
// regardless of type. True when there is at least one A2A task being polled
// OR at least one running background shell. This is the cross-type query
// the BackgroundTasksWaiter uses to decide whether the session is safe to
// close.
func (r *backgroundTaskRegistry) HasPending() bool {
	if len(r.GetAllPollingTasks()) > 0 {
		return true
	}
	if r.ShellTracker != nil && r.CountRunning() > 0 {
		return true
	}
	return false
}
