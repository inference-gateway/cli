package utils

import (
	"sync"

	domain "github.com/inference-gateway/cli/internal/domain"
)

var _ domain.TaskTracker = (*TaskTrackerImpl)(nil)

// AgentContext represents a context within an agent with its tasks
type AgentContext struct {
	ContextID string
	Tasks     []*domain.TaskPollingState
}

// Agent represents an A2A agent with its contexts
type Agent struct {
	AgentURL string
	Contexts []*AgentContext
}

// TaskTrackerImpl provides a hierarchical implementation of TaskTracker
type TaskTrackerImpl struct {
	mu sync.RWMutex

	// Hierarchical structure
	agents []*Agent

	agentIndex   map[string]int
	contextIndex map[string]*AgentContext
	taskIndex    map[string]*domain.TaskPollingState
}

// NewTaskTracker creates a new TaskTrackerImpl
func NewTaskTracker() domain.TaskTracker {
	return &TaskTrackerImpl{
		agents:       make([]*Agent, 0),
		agentIndex:   make(map[string]int),
		contextIndex: make(map[string]*AgentContext),
		taskIndex:    make(map[string]*domain.TaskPollingState),
	}
}

// RegisterContext registers a server-generated context ID for an agent
func (t *TaskTrackerImpl) RegisterContext(agentURL, contextID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if contextID == "" || agentURL == "" {
		return
	}

	if _, exists := t.contextIndex[contextID]; exists {
		return
	}

	var agent *Agent
	if idx, exists := t.agentIndex[agentURL]; exists {
		agent = t.agents[idx]
	} else {
		agent = &Agent{
			AgentURL: agentURL,
			Contexts: make([]*AgentContext, 0),
		}
		t.agents = append(t.agents, agent)
		t.agentIndex[agentURL] = len(t.agents) - 1
	}

	context := &AgentContext{
		ContextID: contextID,
		Tasks:     make([]*domain.TaskPollingState, 0),
	}
	agent.Contexts = append(agent.Contexts, context)
	t.contextIndex[contextID] = context
}

// GetContextsForAgent returns all context IDs for a specific agent
func (t *TaskTrackerImpl) GetContextsForAgent(agentURL string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	idx, exists := t.agentIndex[agentURL]
	if !exists {
		return []string{}
	}

	agent := t.agents[idx]
	contexts := make([]string, len(agent.Contexts))
	for i, ctx := range agent.Contexts {
		contexts[i] = ctx.ContextID
	}

	return contexts
}

// GetAgentForContext returns the agent URL for a given context ID
func (t *TaskTrackerImpl) GetAgentForContext(contextID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, agent := range t.agents {
		for _, ctx := range agent.Contexts {
			if ctx.ContextID == contextID {
				return agent.AgentURL
			}
		}
	}

	return ""
}

// GetLatestContextForAgent returns the most recently registered context for an agent
func (t *TaskTrackerImpl) GetLatestContextForAgent(agentURL string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	idx, exists := t.agentIndex[agentURL]
	if !exists {
		return ""
	}

	agent := t.agents[idx]
	if len(agent.Contexts) == 0 {
		return ""
	}

	return agent.Contexts[len(agent.Contexts)-1].ContextID
}

// HasContext checks if a context ID is registered
func (t *TaskTrackerImpl) HasContext(contextID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.contextIndex[contextID]
	return exists
}

// RemoveContext removes a context and all its tasks
func (t *TaskTrackerImpl) RemoveContext(contextID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ctx, exists := t.contextIndex[contextID]
	if !exists {
		return
	}

	for _, task := range ctx.Tasks {
		if task.CancelFunc != nil {
			task.CancelFunc()
		}
		delete(t.taskIndex, task.TaskID)
	}

	for agentIdx, agent := range t.agents {
		for ctxIdx, agentCtx := range agent.Contexts {
			if agentCtx.ContextID == contextID {
				agent.Contexts = append(agent.Contexts[:ctxIdx], agent.Contexts[ctxIdx+1:]...)

				if len(agent.Contexts) == 0 {
					t.agents = append(t.agents[:agentIdx], t.agents[agentIdx+1:]...)
					delete(t.agentIndex, agent.AgentURL)

					for i, a := range t.agents {
						t.agentIndex[a.AgentURL] = i
					}
				}

				break
			}
		}
	}

	delete(t.contextIndex, contextID)
}

// AddTask adds a server-generated task ID to a context
func (t *TaskTrackerImpl) AddTask(contextID, taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if contextID == "" || taskID == "" {
		return
	}

	ctx, exists := t.contextIndex[contextID]
	if !exists {
		return
	}

	for _, task := range ctx.Tasks {
		if task.TaskID == taskID {
			return
		}
	}

	state := &domain.TaskPollingState{
		TaskID:    taskID,
		ContextID: contextID,
	}

	ctx.Tasks = append(ctx.Tasks, state)
	t.taskIndex[taskID] = state
}

// GetTasksForContext returns all task IDs for a specific context
func (t *TaskTrackerImpl) GetTasksForContext(contextID string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ctx, exists := t.contextIndex[contextID]
	if !exists {
		return []string{}
	}

	taskIDs := make([]string, len(ctx.Tasks))
	for i, task := range ctx.Tasks {
		taskIDs[i] = task.TaskID
	}

	return taskIDs
}

// GetLatestTaskForContext returns the most recently added task for a context
func (t *TaskTrackerImpl) GetLatestTaskForContext(contextID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ctx, exists := t.contextIndex[contextID]
	if !exists || len(ctx.Tasks) == 0 {
		return ""
	}

	return ctx.Tasks[len(ctx.Tasks)-1].TaskID
}

// GetContextForTask returns the context ID for a given task
func (t *TaskTrackerImpl) GetContextForTask(taskID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	task, exists := t.taskIndex[taskID]
	if !exists {
		return ""
	}

	return task.ContextID
}

// RemoveTask removes a task from its context
func (t *TaskTrackerImpl) RemoveTask(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	task, exists := t.taskIndex[taskID]
	if !exists {
		return
	}

	if task.CancelFunc != nil {
		task.CancelFunc()
	}

	ctx := t.contextIndex[task.ContextID]
	if ctx != nil {
		for i, t := range ctx.Tasks {
			if t.TaskID == taskID {
				ctx.Tasks = append(ctx.Tasks[:i], ctx.Tasks[i+1:]...)
				break
			}
		}
	}

	delete(t.taskIndex, taskID)
}

// HasTask checks if a task ID exists
func (t *TaskTrackerImpl) HasTask(taskID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.taskIndex[taskID]
	return exists
}

// GetAllAgents returns all agent URLs being tracked
func (t *TaskTrackerImpl) GetAllAgents() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	agents := make([]string, len(t.agents))
	for i, agent := range t.agents {
		agents[i] = agent.AgentURL
	}

	return agents
}

// GetAllContexts returns all context IDs being tracked
func (t *TaskTrackerImpl) GetAllContexts() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	contexts := make([]string, 0, len(t.contextIndex))
	for contextID := range t.contextIndex {
		contexts = append(contexts, contextID)
	}

	return contexts
}

// ClearAllAgents clears all tracked agents, contexts, tasks, and polling states
func (t *TaskTrackerImpl) ClearAllAgents() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, task := range t.taskIndex {
		if task.CancelFunc != nil {
			task.CancelFunc()
		}
	}

	t.agents = make([]*Agent, 0)
	t.agentIndex = make(map[string]int)
	t.contextIndex = make(map[string]*AgentContext)
	t.taskIndex = make(map[string]*domain.TaskPollingState)
}

// StartPolling starts tracking a background polling operation for a task
func (t *TaskTrackerImpl) StartPolling(taskID string, state *domain.TaskPollingState) {
	if state == nil || taskID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if existingState, exists := t.taskIndex[taskID]; exists {
		if existingState.CancelFunc != nil {
			existingState.CancelFunc()
		}
	}

	ctx, exists := t.contextIndex[state.ContextID]
	if !exists {
		return
	}

	state.IsPolling = true

	found := false
	for i, task := range ctx.Tasks {
		if task.TaskID == taskID {
			ctx.Tasks[i] = state
			found = true
			break
		}
	}

	if !found {
		ctx.Tasks = append(ctx.Tasks, state)
	}

	t.taskIndex[taskID] = state
}

// StopPolling stops and clears the polling state for a task
func (t *TaskTrackerImpl) StopPolling(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	task, exists := t.taskIndex[taskID]
	if !exists {
		return
	}

	if task.CancelFunc != nil {
		task.CancelFunc()
	}

	task.IsPolling = false

	ctx := t.contextIndex[task.ContextID]
	if ctx != nil {
		for i, t := range ctx.Tasks {
			if t.TaskID == taskID {
				ctx.Tasks = append(ctx.Tasks[:i], ctx.Tasks[i+1:]...)
				break
			}
		}
	}

	delete(t.taskIndex, taskID)
}

// GetPollingState returns the current polling state for a task
func (t *TaskTrackerImpl) GetPollingState(taskID string) *domain.TaskPollingState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.taskIndex[taskID]
}

// IsPolling returns whether a task currently has an active polling operation
func (t *TaskTrackerImpl) IsPolling(taskID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	task, exists := t.taskIndex[taskID]
	if !exists {
		return false
	}

	return task.IsPolling
}

// GetPollingTasksForContext returns all task IDs that are currently being polled for a context
func (t *TaskTrackerImpl) GetPollingTasksForContext(contextID string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ctx, exists := t.contextIndex[contextID]
	if !exists {
		return []string{}
	}

	pollingTasks := make([]string, 0)
	for _, task := range ctx.Tasks {
		if task.IsPolling {
			pollingTasks = append(pollingTasks, task.TaskID)
		}
	}

	return pollingTasks
}

// GetAllPollingTasks returns all task IDs that are currently being polled
func (t *TaskTrackerImpl) GetAllPollingTasks() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	taskIDs := make([]string, 0)

	for _, agent := range t.agents {
		for _, ctx := range agent.Contexts {
			for _, task := range ctx.Tasks {
				if task.IsPolling {
					taskIDs = append(taskIDs, task.TaskID)
				}
			}
		}
	}

	return taskIDs
}
