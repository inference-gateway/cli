package utils

import (
	"sort"
	"sync"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// SimpleTaskTracker provides a simple in-memory implementation of TaskTracker
type SimpleTaskTracker struct {
	mu              sync.RWMutex
	agentTaskIDs    map[string]string
	agentContextIDs map[string]string
	pollingStates   map[string]*domain.TaskPollingState
}

// NewSimpleTaskTracker creates a new SimpleTaskTracker
func NewSimpleTaskTracker() domain.TaskTracker {
	return &SimpleTaskTracker{
		agentTaskIDs:    make(map[string]string),
		agentContextIDs: make(map[string]string),
		pollingStates:   make(map[string]*domain.TaskPollingState),
	}
}

// GetTaskIDForAgent returns the task ID for a specific agent
func (t *SimpleTaskTracker) GetTaskIDForAgent(agentURL string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.agentTaskIDs[agentURL]
}

// SetTaskIDForAgent sets the task ID for a specific agent
func (t *SimpleTaskTracker) SetTaskIDForAgent(agentURL, taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if taskID != "" {
		t.agentTaskIDs[agentURL] = taskID
	}
}

// ClearTaskIDForAgent clears the task ID for a specific agent
func (t *SimpleTaskTracker) ClearTaskIDForAgent(agentURL string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.agentTaskIDs, agentURL)
}

// GetContextIDForAgent returns the context ID for a specific agent
func (t *SimpleTaskTracker) GetContextIDForAgent(agentURL string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.agentContextIDs[agentURL]
}

// SetContextIDForAgent sets the context ID for a specific agent
func (t *SimpleTaskTracker) SetContextIDForAgent(agentURL, contextID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if contextID != "" {
		t.agentContextIDs[agentURL] = contextID
	}
}

// ClearAllAgents clears all tracked task and context IDs for all agents
func (t *SimpleTaskTracker) ClearAllAgents() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for agentURL, state := range t.pollingStates {
		if state.CancelFunc != nil {
			state.CancelFunc()
		}
		delete(t.pollingStates, agentURL)
	}

	t.agentTaskIDs = make(map[string]string)
	t.agentContextIDs = make(map[string]string)
	t.pollingStates = make(map[string]*domain.TaskPollingState)
}

// StartPolling starts tracking a background polling operation for an agent
func (t *SimpleTaskTracker) StartPolling(agentURL string, state *domain.TaskPollingState) {
	if state == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if existingState, exists := t.pollingStates[agentURL]; exists {
		if existingState.CancelFunc != nil {
			existingState.CancelFunc()
		}
	}

	state.IsPolling = true
	t.pollingStates[agentURL] = state
}

// StopPolling stops and clears the polling state for an agent
func (t *SimpleTaskTracker) StopPolling(agentURL string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if state, exists := t.pollingStates[agentURL]; exists {
		if state.CancelFunc != nil {
			state.CancelFunc()
		}
		state.IsPolling = false
		delete(t.pollingStates, agentURL)
	}
}

// GetPollingState returns the current polling state for an agent
func (t *SimpleTaskTracker) GetPollingState(agentURL string) *domain.TaskPollingState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.pollingStates[agentURL]
}

// IsPolling returns whether an agent currently has an active polling operation
func (t *SimpleTaskTracker) IsPolling(agentURL string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if state, exists := t.pollingStates[agentURL]; exists {
		return state.IsPolling
	}
	return false
}

func (t *SimpleTaskTracker) GetAllPollingAgents() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	agents := make([]string, 0, len(t.pollingStates))
	for agentURL, state := range t.pollingStates {
		if state.IsPolling {
			agents = append(agents, agentURL)
		}
	}

	sort.Strings(agents)

	return agents
}
