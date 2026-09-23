package infrastructure

import (
	"sync"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

var _ scheddomain.A2ATaskTracker = (*A2ATaskTracker)(nil)

// a2aContext represents a context within an agent with its tasks
type a2aContext struct {
	ContextID string
	Tasks     []*scheddomain.TaskPollingState
}

// a2aAgent represents an A2A agent with its contexts
type a2aAgent struct {
	AgentURL string
	Contexts []*a2aContext
}

// A2ATaskTracker is the hierarchical (agent → context → task) scheddomain.A2ATaskTracker.
type A2ATaskTracker struct {
	mu sync.RWMutex

	// Hierarchical structure
	agents []*a2aAgent

	agentIndex   map[string]int
	contextIndex map[string]*a2aContext
	taskIndex    map[string]*scheddomain.TaskPollingState
}

// NewA2ATaskTracker creates a new A2ATaskTracker. Returns the concrete
// type so callers that need to embed it (e.g. the unified
// services.BackgroundTaskRegistry) don't need to type-assert. The result
// still satisfies scheddomain.A2ATaskTracker via interface conversion.
func NewA2ATaskTracker() *A2ATaskTracker {
	return &A2ATaskTracker{
		agents:       make([]*a2aAgent, 0),
		agentIndex:   make(map[string]int),
		contextIndex: make(map[string]*a2aContext),
		taskIndex:    make(map[string]*scheddomain.TaskPollingState),
	}
}

// RegisterContext registers a server-generated context ID for an agent
func (t *A2ATaskTracker) RegisterContext(agentURL, contextID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if contextID == "" || agentURL == "" {
		return
	}

	if _, exists := t.contextIndex[contextID]; exists {
		return
	}

	var agent *a2aAgent
	if idx, exists := t.agentIndex[agentURL]; exists {
		agent = t.agents[idx]
	} else {
		agent = &a2aAgent{
			AgentURL: agentURL,
			Contexts: make([]*a2aContext, 0),
		}
		t.agents = append(t.agents, agent)
		t.agentIndex[agentURL] = len(t.agents) - 1
	}

	context := &a2aContext{
		ContextID: contextID,
		Tasks:     make([]*scheddomain.TaskPollingState, 0),
	}
	agent.Contexts = append(agent.Contexts, context)
	t.contextIndex[contextID] = context
}

// GetContextsForAgent returns all context IDs for a specific agent
func (t *A2ATaskTracker) GetContextsForAgent(agentURL string) []string {
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
func (t *A2ATaskTracker) GetAgentForContext(contextID string) string {
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
func (t *A2ATaskTracker) GetLatestContextForAgent(agentURL string) string {
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
func (t *A2ATaskTracker) HasContext(contextID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.contextIndex[contextID]
	return exists
}

// RemoveContext removes a context and all its tasks
func (t *A2ATaskTracker) RemoveContext(contextID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ctx, exists := t.contextIndex[contextID]
	if !exists {
		return
	}

	for _, task := range ctx.Tasks {
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
func (t *A2ATaskTracker) AddTask(contextID, taskID string) {
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

	state := &scheddomain.TaskPollingState{
		TaskID:    taskID,
		ContextID: contextID,
	}

	ctx.Tasks = append(ctx.Tasks, state)
	t.taskIndex[taskID] = state
}

// GetTasksForContext returns all task IDs for a specific context
func (t *A2ATaskTracker) GetTasksForContext(contextID string) []string {
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
func (t *A2ATaskTracker) GetLatestTaskForContext(contextID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ctx, exists := t.contextIndex[contextID]
	if !exists || len(ctx.Tasks) == 0 {
		return ""
	}

	return ctx.Tasks[len(ctx.Tasks)-1].TaskID
}

// GetContextForTask returns the context ID for a given task
func (t *A2ATaskTracker) GetContextForTask(taskID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	task, exists := t.taskIndex[taskID]
	if !exists {
		return ""
	}

	return task.ContextID
}

// RemoveTask removes a task from its context
func (t *A2ATaskTracker) RemoveTask(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	task, exists := t.taskIndex[taskID]
	if !exists {
		return
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
func (t *A2ATaskTracker) HasTask(taskID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.taskIndex[taskID]
	return exists
}

// GetAllAgents returns all agent URLs being tracked
func (t *A2ATaskTracker) GetAllAgents() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	agents := make([]string, len(t.agents))
	for i, agent := range t.agents {
		agents[i] = agent.AgentURL
	}

	return agents
}

// GetAllContexts returns all context IDs being tracked
func (t *A2ATaskTracker) GetAllContexts() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	contexts := make([]string, 0, len(t.contextIndex))
	for contextID := range t.contextIndex {
		contexts = append(contexts, contextID)
	}

	return contexts
}

// ClearAllAgents clears all tracked agents, contexts, tasks, and polling states
func (t *A2ATaskTracker) ClearAllAgents() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.agents = make([]*a2aAgent, 0)
	t.agentIndex = make(map[string]int)
	t.contextIndex = make(map[string]*a2aContext)
	t.taskIndex = make(map[string]*scheddomain.TaskPollingState)
}

// StartPolling starts tracking a background polling operation for a task
func (t *A2ATaskTracker) StartPolling(taskID string, state *scheddomain.TaskPollingState) {
	if state == nil || taskID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

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
func (t *A2ATaskTracker) StopPolling(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	task, exists := t.taskIndex[taskID]
	if !exists {
		return
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
func (t *A2ATaskTracker) GetPollingState(taskID string) *scheddomain.TaskPollingState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.taskIndex[taskID]
}

// GetPollingTasksForContext returns all task IDs that are currently being polled for a context
func (t *A2ATaskTracker) GetPollingTasksForContext(contextID string) []string {
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
