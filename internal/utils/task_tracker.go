package utils

import (
	"sort"
	"sync"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

var _ domain.TaskTracker = (*TaskTrackerImpl)(nil)

// TaskTrackerImpl provides a simple in-memory implementation of TaskTracker
// Supports multi-tenant: multiple contexts per agent, tasks scoped to contexts
type TaskTrackerImpl struct {
	mu sync.RWMutex

	// Multi-tenant context tracking
	agentContexts  map[string][]string
	contextToAgent map[string]string

	// Task tracking (tasks scoped to contexts per A2A spec)
	contextTasks  map[string][]string
	taskToContext map[string]string

	// Polling state (keyed by task ID)
	pollingStates map[string]*domain.TaskPollingState
}

// NewTaskTracker creates a new TaskTrackerImpl
func NewTaskTracker() domain.TaskTracker {
	return &TaskTrackerImpl{
		agentContexts:  make(map[string][]string),
		contextToAgent: make(map[string]string),
		contextTasks:   make(map[string][]string),
		taskToContext:  make(map[string]string),
		pollingStates:  make(map[string]*domain.TaskPollingState),
	}
}

// RegisterContext registers a server-generated context ID for an agent
func (t *TaskTrackerImpl) RegisterContext(agentURL, contextID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if contextID == "" || agentURL == "" {
		return
	}

	contexts := t.agentContexts[agentURL]
	for _, existingID := range contexts {
		if existingID == contextID {
			return
		}
	}

	// Register context
	t.agentContexts[agentURL] = append(contexts, contextID)
	t.contextToAgent[contextID] = agentURL
}

// GetContextsForAgent returns all context IDs for a specific agent
func (t *TaskTrackerImpl) GetContextsForAgent(agentURL string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	contexts := t.agentContexts[agentURL]
	if contexts == nil {
		return []string{}
	}

	result := make([]string, len(contexts))
	copy(result, contexts)
	return result
}

// GetAgentForContext returns the agent URL for a given context ID
func (t *TaskTrackerImpl) GetAgentForContext(contextID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.contextToAgent[contextID]
}

// GetLatestContextForAgent returns the most recently registered context for an agent
func (t *TaskTrackerImpl) GetLatestContextForAgent(agentURL string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	contexts := t.agentContexts[agentURL]
	if len(contexts) == 0 {
		return ""
	}

	return contexts[len(contexts)-1]
}

// HasContext checks if a context ID is registered
func (t *TaskTrackerImpl) HasContext(contextID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, exists := t.contextToAgent[contextID]
	return exists
}

// RemoveContext removes a context and all its tasks
func (t *TaskTrackerImpl) RemoveContext(contextID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	agentURL := t.contextToAgent[contextID]
	if agentURL == "" {
		return
	}

	tasks := t.contextTasks[contextID]
	for _, taskID := range tasks {
		delete(t.taskToContext, taskID)

		if state, exists := t.pollingStates[taskID]; exists {
			if state.CancelFunc != nil {
				state.CancelFunc()
			}
			delete(t.pollingStates, taskID)
		}
	}
	delete(t.contextTasks, contextID)

	contexts := t.agentContexts[agentURL]
	newContexts := make([]string, 0, len(contexts))
	for _, id := range contexts {
		if id != contextID {
			newContexts = append(newContexts, id)
		}
	}

	if len(newContexts) == 0 {
		delete(t.agentContexts, agentURL)
	} else {
		t.agentContexts[agentURL] = newContexts
	}

	delete(t.contextToAgent, contextID)
}

// AddTask adds a server-generated task ID to a context
func (t *TaskTrackerImpl) AddTask(contextID, taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if contextID == "" || taskID == "" {
		return
	}

	if _, exists := t.contextToAgent[contextID]; !exists {
		return
	}

	tasks := t.contextTasks[contextID]
	for _, existingID := range tasks {
		if existingID == taskID {
			return
		}
	}

	t.contextTasks[contextID] = append(tasks, taskID)
	t.taskToContext[taskID] = contextID
}

// GetTasksForContext returns all task IDs for a specific context
func (t *TaskTrackerImpl) GetTasksForContext(contextID string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tasks := t.contextTasks[contextID]
	if tasks == nil {
		return []string{}
	}

	result := make([]string, len(tasks))
	copy(result, tasks)
	return result
}

// GetLatestTaskForContext returns the most recently added task for a context
func (t *TaskTrackerImpl) GetLatestTaskForContext(contextID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tasks := t.contextTasks[contextID]
	if len(tasks) == 0 {
		return ""
	}

	return tasks[len(tasks)-1]
}

// GetContextForTask returns the context ID for a given task
func (t *TaskTrackerImpl) GetContextForTask(taskID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.taskToContext[taskID]
}

// RemoveTask removes a task from its context
func (t *TaskTrackerImpl) RemoveTask(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	contextID := t.taskToContext[taskID]
	if contextID == "" {
		return
	}

	tasks := t.contextTasks[contextID]
	newTasks := make([]string, 0, len(tasks))
	for _, id := range tasks {
		if id != taskID {
			newTasks = append(newTasks, id)
		}
	}

	if len(newTasks) == 0 {
		delete(t.contextTasks, contextID)
	} else {
		t.contextTasks[contextID] = newTasks
	}

	delete(t.taskToContext, taskID)

	if state, exists := t.pollingStates[taskID]; exists {
		if state.CancelFunc != nil {
			state.CancelFunc()
		}
		delete(t.pollingStates, taskID)
	}
}

// HasTask checks if a task ID exists
func (t *TaskTrackerImpl) HasTask(taskID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, exists := t.taskToContext[taskID]
	return exists
}

// GetAllAgents returns all agent URLs being tracked
func (t *TaskTrackerImpl) GetAllAgents() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	agents := make([]string, 0, len(t.agentContexts))
	for agentURL := range t.agentContexts {
		agents = append(agents, agentURL)
	}

	sort.Strings(agents)
	return agents
}

// GetAllContexts returns all context IDs being tracked
func (t *TaskTrackerImpl) GetAllContexts() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	contexts := make([]string, 0, len(t.contextToAgent))
	for contextID := range t.contextToAgent {
		contexts = append(contexts, contextID)
	}

	sort.Strings(contexts)
	return contexts
}

// ClearAllAgents clears all tracked agents, contexts, tasks, and polling states
func (t *TaskTrackerImpl) ClearAllAgents() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for taskID, state := range t.pollingStates {
		if state.CancelFunc != nil {
			state.CancelFunc()
		}
		delete(t.pollingStates, taskID)
	}

	t.agentContexts = make(map[string][]string)
	t.contextToAgent = make(map[string]string)
	t.contextTasks = make(map[string][]string)
	t.taskToContext = make(map[string]string)
	t.pollingStates = make(map[string]*domain.TaskPollingState)
}

// StartPolling starts tracking a background polling operation for a task
func (t *TaskTrackerImpl) StartPolling(taskID string, state *domain.TaskPollingState) {
	if state == nil || taskID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if existingState, exists := t.pollingStates[taskID]; exists {
		if existingState.CancelFunc != nil {
			existingState.CancelFunc()
		}
	}

	state.IsPolling = true
	t.pollingStates[taskID] = state
}

// StopPolling stops and clears the polling state for a task
func (t *TaskTrackerImpl) StopPolling(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if state, exists := t.pollingStates[taskID]; exists {
		if state.CancelFunc != nil {
			state.CancelFunc()
		}
		state.IsPolling = false
		delete(t.pollingStates, taskID)
	}
}

// GetPollingState returns the current polling state for a task
func (t *TaskTrackerImpl) GetPollingState(taskID string) *domain.TaskPollingState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.pollingStates[taskID]
}

// IsPolling returns whether a task currently has an active polling operation
func (t *TaskTrackerImpl) IsPolling(taskID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if state, exists := t.pollingStates[taskID]; exists {
		return state.IsPolling
	}
	return false
}

// GetPollingTasksForContext returns all task IDs that are currently being polled for a context
func (t *TaskTrackerImpl) GetPollingTasksForContext(contextID string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tasks := t.contextTasks[contextID]
	if tasks == nil {
		return []string{}
	}

	pollingTasks := make([]string, 0)
	for _, taskID := range tasks {
		if state, exists := t.pollingStates[taskID]; exists && state.IsPolling {
			pollingTasks = append(pollingTasks, taskID)
		}
	}

	sort.Slice(pollingTasks, func(i, j int) bool {
		stateI := t.pollingStates[pollingTasks[i]]
		stateJ := t.pollingStates[pollingTasks[j]]
		return stateI.StartedAt.Before(stateJ.StartedAt)
	})

	return pollingTasks
}

// GetAllPollingTasks returns all task IDs that are currently being polled
func (t *TaskTrackerImpl) GetAllPollingTasks() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tasks := make([]taskWithTime, 0, len(t.pollingStates))
	for taskID, state := range t.pollingStates {
		if state.IsPolling {
			tasks = append(tasks, taskWithTime{
				taskID:    taskID,
				startedAt: state.StartedAt,
			})
		}
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].startedAt.Before(tasks[j].startedAt)
	})

	result := make([]string, len(tasks))
	for i, t := range tasks {
		result[i] = t.taskID
	}

	return result
}

// taskWithTime is a helper struct for sorting tasks by start time
type taskWithTime struct {
	taskID    string
	startedAt time.Time
}
