package utils

import (
	"fmt"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
)

// shellTracker implements the domain.ShellTracker interface.
type shellTracker struct {
	shells        map[string]*domain.BackgroundShell
	maxConcurrent int
	mutex         sync.RWMutex
}

// NewShellTracker creates a new shell tracker with the specified maximum concurrent shells.
func NewShellTracker(maxConcurrent int) domain.ShellTracker {
	return &shellTracker{
		shells:        make(map[string]*domain.BackgroundShell),
		maxConcurrent: maxConcurrent,
	}
}

// Add adds a new shell to the tracker.
// Returns an error if max concurrent limit is reached.
func (st *shellTracker) Add(shell *domain.BackgroundShell) error {
	st.mutex.Lock()
	defer st.mutex.Unlock()

	if shell.State == domain.ShellStateRunning {
		runningCount := 0
		for _, s := range st.shells {
			if s.State == domain.ShellStateRunning {
				runningCount++
			}
		}

		if runningCount >= st.maxConcurrent {
			return fmt.Errorf("maximum concurrent shells limit reached (%d)", st.maxConcurrent)
		}
	}

	if _, exists := st.shells[shell.ShellID]; exists {
		return fmt.Errorf("shell with ID %s already exists", shell.ShellID)
	}

	st.shells[shell.ShellID] = shell

	return nil
}

// Get retrieves a shell by ID.
// Returns nil if not found.
func (st *shellTracker) Get(shellID string) *domain.BackgroundShell {
	st.mutex.RLock()
	defer st.mutex.RUnlock()

	return st.shells[shellID]
}

// GetAll returns all tracked shells.
func (st *shellTracker) GetAll() []*domain.BackgroundShell {
	st.mutex.RLock()
	defer st.mutex.RUnlock()

	shells := make([]*domain.BackgroundShell, 0, len(st.shells))

	for _, shell := range st.shells {
		shells = append(shells, shell)
	}

	return shells
}

// Remove removes a shell from the tracker.
func (st *shellTracker) Remove(shellID string) error {
	st.mutex.Lock()
	defer st.mutex.Unlock()

	if _, exists := st.shells[shellID]; !exists {
		return fmt.Errorf("shell with ID %s not found", shellID)
	}

	delete(st.shells, shellID)

	return nil
}

// Cleanup removes shells in terminal states older than the specified duration.
// Returns the number of shells removed.
func (st *shellTracker) Cleanup(olderThan time.Duration) int {
	st.mutex.Lock()
	defer st.mutex.Unlock()

	cutoffTime := time.Now().Add(-olderThan)
	removed := 0

	for id, shell := range st.shells {
		if !shell.State.IsTerminal() {
			continue
		}

		completionTime := shell.StartedAt
		if shell.CompletedAt != nil {
			completionTime = *shell.CompletedAt
		}

		if completionTime.Before(cutoffTime) {
			delete(st.shells, id)
			removed++
		}
	}

	return removed
}

// Count returns the number of tracked shells.
func (st *shellTracker) Count() int {
	st.mutex.RLock()
	defer st.mutex.RUnlock()

	return len(st.shells)
}

// CountRunning returns the number of shells in running state.
func (st *shellTracker) CountRunning() int {
	st.mutex.RLock()
	defer st.mutex.RUnlock()

	count := 0

	for _, shell := range st.shells {
		if shell.State == domain.ShellStateRunning {
			count++
		}
	}

	return count
}
