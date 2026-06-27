package utils

import (
	"fmt"
	"sync"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// subagentTracker implements domain.SubagentTracker with a flat map guarded by
// an RWMutex (modeled on shellTracker). Concurrency limits are enforced by the
// Agent tool, so no max is kept here.
type subagentTracker struct {
	subagents map[string]*domain.SubagentState
	mutex     sync.RWMutex
}

// NewSubagentTracker creates an empty subagent tracker.
func NewSubagentTracker() domain.SubagentTracker {
	return &subagentTracker{
		subagents: make(map[string]*domain.SubagentState),
	}
}

// AddSubagent registers a running subagent. Returns an error if the ID exists.
func (t *subagentTracker) AddSubagent(state *domain.SubagentState) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if state == nil {
		return fmt.Errorf("subagent state is nil")
	}
	if _, exists := t.subagents[state.ID]; exists {
		return fmt.Errorf("subagent with ID %s already exists", state.ID)
	}
	t.subagents[state.ID] = state
	return nil
}

// GetSubagent returns a subagent by ID, or nil if not tracked.
func (t *subagentTracker) GetSubagent(id string) *domain.SubagentState {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	return t.subagents[id]
}

// GetAllSubagents returns all tracked subagents.
func (t *subagentTracker) GetAllSubagents() []*domain.SubagentState {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	out := make([]*domain.SubagentState, 0, len(t.subagents))
	for _, s := range t.subagents {
		out = append(out, s)
	}
	return out
}

// RemoveSubagent removes a subagent from tracking.
func (t *subagentTracker) RemoveSubagent(id string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if _, exists := t.subagents[id]; !exists {
		return fmt.Errorf("subagent with ID %s not found", id)
	}
	delete(t.subagents, id)
	return nil
}

// SetSubagentStatus atomically updates a subagent's status under the tracker's
// lock. Returns an error if the ID is not tracked.
func (t *subagentTracker) SetSubagentStatus(id string, status domain.SubagentStatus) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	s, exists := t.subagents[id]
	if !exists {
		return fmt.Errorf("subagent with ID %s not found", id)
	}
	s.Status = status
	return nil
}

// CountRunningSubagents returns the number of subagents in the running state.
func (t *subagentTracker) CountRunningSubagents() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	count := 0
	for _, s := range t.subagents {
		if s.Status == domain.SubagentRunning {
			count++
		}
	}
	return count
}
