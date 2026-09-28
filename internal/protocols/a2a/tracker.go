package a2a

import (
	"sync"

	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

var _ a2adomain.TaskTracker = (*TaskTracker)(nil)

// a2aContext represents a context within an agent with its tasks
type a2aContext struct {
	ContextID string
	Tasks     []*a2adomain.TaskPollingState
}

// a2aAgent represents an A2A agent with its contexts
type a2aAgent struct {
	AgentURL string
	Contexts []*a2aContext
}

// jobDiscarder stops and forgets every supervised job of one kind.
// *jobs.Supervisor satisfies it.
type jobDiscarder interface {
	DiscardKind(kind scheddomain.JobKind)
}

// TaskTracker is the hierarchical (agent → context → task) a2adomain.TaskTracker.
type TaskTracker struct {
	mu   sync.RWMutex
	jobs jobDiscarder

	// Hierarchical structure
	agents []*a2aAgent

	agentIndex   map[string]int
	contextIndex map[string]*a2aContext
	taskIndex    map[string]*a2adomain.TaskPollingState
}

// NewTaskTracker creates a new TaskTracker. jobs, when set, is the supervisor
// whose A2A jobs ClearAllAgents discards along with the graph.
func NewTaskTracker(jobs jobDiscarder) *TaskTracker {
	return &TaskTracker{
		jobs:         jobs,
		agents:       make([]*a2aAgent, 0),
		agentIndex:   make(map[string]int),
		contextIndex: make(map[string]*a2aContext),
		taskIndex:    make(map[string]*a2adomain.TaskPollingState),
	}
}

// RegisterContext registers a server-generated context ID for an agent
func (t *TaskTracker) RegisterContext(agentURL, contextID string) {
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
		Tasks:     make([]*a2adomain.TaskPollingState, 0),
	}
	agent.Contexts = append(agent.Contexts, context)
	t.contextIndex[contextID] = context
}

// GetContextsForAgent returns all context IDs for a specific agent
func (t *TaskTracker) GetContextsForAgent(agentURL string) []string {
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
func (t *TaskTracker) GetAgentForContext(contextID string) string {
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
func (t *TaskTracker) GetLatestContextForAgent(agentURL string) string {
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
func (t *TaskTracker) HasContext(contextID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.contextIndex[contextID]
	return exists
}

// RemoveContext removes a context and all its tasks
func (t *TaskTracker) RemoveContext(contextID string) {
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
func (t *TaskTracker) AddTask(contextID, taskID string) {
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

	state := &a2adomain.TaskPollingState{
		TaskID:    taskID,
		ContextID: contextID,
	}

	ctx.Tasks = append(ctx.Tasks, state)
	t.taskIndex[taskID] = state
}

// GetTasksForContext returns all task IDs for a specific context
func (t *TaskTracker) GetTasksForContext(contextID string) []string {
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
func (t *TaskTracker) GetLatestTaskForContext(contextID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ctx, exists := t.contextIndex[contextID]
	if !exists || len(ctx.Tasks) == 0 {
		return ""
	}

	return ctx.Tasks[len(ctx.Tasks)-1].TaskID
}

// GetContextForTask returns the context ID for a given task
func (t *TaskTracker) GetContextForTask(taskID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	task, exists := t.taskIndex[taskID]
	if !exists {
		return ""
	}

	return task.ContextID
}

// RemoveTask removes a task from its context
func (t *TaskTracker) RemoveTask(taskID string) {
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
func (t *TaskTracker) HasTask(taskID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.taskIndex[taskID]
	return exists
}

// GetAllAgents returns all agent URLs being tracked
func (t *TaskTracker) GetAllAgents() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	agents := make([]string, len(t.agents))
	for i, agent := range t.agents {
		agents[i] = agent.AgentURL
	}

	return agents
}

// GetAllContexts returns all context IDs being tracked
func (t *TaskTracker) GetAllContexts() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	contexts := make([]string, 0, len(t.contextIndex))
	for contextID := range t.contextIndex {
		contexts = append(contexts, contextID)
	}

	return contexts
}

// ClearAllAgents clears all tracked agents, contexts, tasks, and polling states,
// and discards the in-flight supervised A2A jobs, so a conversation clear or
// switch cannot leave orphaned pollers running (still counted in the status
// bar, listed in /tasks, and able to land a late completion note).
func (t *TaskTracker) ClearAllAgents() {
	if t.jobs != nil {
		t.jobs.DiscardKind(scheddomain.JobKindA2A)
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.agents = make([]*a2aAgent, 0)
	t.agentIndex = make(map[string]int)
	t.contextIndex = make(map[string]*a2aContext)
	t.taskIndex = make(map[string]*a2adomain.TaskPollingState)
}

// StartPolling starts tracking a background polling operation for a task
func (t *TaskTracker) StartPolling(taskID string, state *a2adomain.TaskPollingState) {
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
func (t *TaskTracker) StopPolling(taskID string) {
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
func (t *TaskTracker) GetPollingState(taskID string) *a2adomain.TaskPollingState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.taskIndex[taskID]
}

// GetPollingTasksForContext returns all task IDs that are currently being polled for a context
func (t *TaskTracker) GetPollingTasksForContext(contextID string) []string {
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
