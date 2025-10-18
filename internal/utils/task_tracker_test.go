package utils

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	assert "github.com/stretchr/testify/assert"
)

func TestTaskTracker_RegisterAndGetContextsForAgent(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"

	contexts := tracker.GetContextsForAgent(agentURL)
	assert.Empty(t, contexts)

	tracker.RegisterContext(agentURL, "context-1")
	contexts = tracker.GetContextsForAgent(agentURL)
	assert.Equal(t, []string{"context-1"}, contexts)

	tracker.RegisterContext(agentURL, "context-2")
	contexts = tracker.GetContextsForAgent(agentURL)
	assert.Equal(t, []string{"context-1", "context-2"}, contexts)

	tracker.RegisterContext(agentURL, "context-3")
	contexts = tracker.GetContextsForAgent(agentURL)
	assert.Equal(t, []string{"context-1", "context-2", "context-3"}, contexts)

	tracker.RegisterContext(agentURL, "context-2")
	contexts = tracker.GetContextsForAgent(agentURL)
	assert.Equal(t, []string{"context-1", "context-2", "context-3"}, contexts)
}

func TestTaskTracker_AddAndGetTasksForContext(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"
	contextID := "context-1"

	// Register context first
	tracker.RegisterContext(agentURL, contextID)

	tasks := tracker.GetTasksForContext(contextID)
	assert.Empty(t, tasks)

	tracker.AddTask(contextID, "task-1")
	tasks = tracker.GetTasksForContext(contextID)
	assert.Equal(t, []string{"task-1"}, tasks)

	tracker.AddTask(contextID, "task-2")
	tasks = tracker.GetTasksForContext(contextID)
	assert.Equal(t, []string{"task-1", "task-2"}, tasks)

	tracker.AddTask(contextID, "task-3")
	tasks = tracker.GetTasksForContext(contextID)
	assert.Equal(t, []string{"task-1", "task-2", "task-3"}, tasks)

	// Duplicate should be ignored
	tracker.AddTask(contextID, "task-2")
	tasks = tracker.GetTasksForContext(contextID)
	assert.Equal(t, []string{"task-1", "task-2", "task-3"}, tasks)
}

func TestTaskTracker_RemoveTask(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"
	contextID := "context-1"

	tracker.RegisterContext(agentURL, contextID)
	tracker.AddTask(contextID, "task-1")
	tracker.AddTask(contextID, "task-2")
	tracker.AddTask(contextID, "task-3")

	tracker.RemoveTask("task-2")
	tasks := tracker.GetTasksForContext(contextID)
	assert.Equal(t, []string{"task-1", "task-3"}, tasks)

	tracker.RemoveTask("task-1")
	tasks = tracker.GetTasksForContext(contextID)
	assert.Equal(t, []string{"task-3"}, tasks)

	tracker.RemoveTask("task-3")
	tasks = tracker.GetTasksForContext(contextID)
	assert.Empty(t, tasks)
}

func TestTaskTracker_RemoveContext(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"
	contextID := "context-1"

	tracker.RegisterContext(agentURL, contextID)
	tracker.AddTask(contextID, "task-1")
	tracker.AddTask(contextID, "task-2")

	assert.True(t, tracker.HasContext(contextID))
	assert.True(t, tracker.HasTask("task-1"))
	assert.True(t, tracker.HasTask("task-2"))

	tracker.RemoveContext(contextID)

	assert.False(t, tracker.HasContext(contextID))
	assert.False(t, tracker.HasTask("task-1"))
	assert.False(t, tracker.HasTask("task-2"))
	assert.Empty(t, tracker.GetTasksForContext(contextID))
	assert.Empty(t, tracker.GetContextsForAgent(agentURL))
}

func TestTaskTracker_GetLatestContextForAgent(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"

	latest := tracker.GetLatestContextForAgent(agentURL)
	assert.Empty(t, latest)

	tracker.RegisterContext(agentURL, "context-1")
	latest = tracker.GetLatestContextForAgent(agentURL)
	assert.Equal(t, "context-1", latest)

	tracker.RegisterContext(agentURL, "context-2")
	latest = tracker.GetLatestContextForAgent(agentURL)
	assert.Equal(t, "context-2", latest)

	tracker.RegisterContext(agentURL, "context-3")
	latest = tracker.GetLatestContextForAgent(agentURL)
	assert.Equal(t, "context-3", latest)
}

func TestTaskTracker_GetLatestTaskForContext(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"
	contextID := "context-1"

	tracker.RegisterContext(agentURL, contextID)

	latest := tracker.GetLatestTaskForContext(contextID)
	assert.Empty(t, latest)

	tracker.AddTask(contextID, "task-1")
	latest = tracker.GetLatestTaskForContext(contextID)
	assert.Equal(t, "task-1", latest)

	tracker.AddTask(contextID, "task-2")
	latest = tracker.GetLatestTaskForContext(contextID)
	assert.Equal(t, "task-2", latest)

	tracker.AddTask(contextID, "task-3")
	latest = tracker.GetLatestTaskForContext(contextID)
	assert.Equal(t, "task-3", latest)
}

func TestTaskTracker_GetContextForTask(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"
	contextID := "context-1"

	tracker.RegisterContext(agentURL, contextID)
	tracker.AddTask(contextID, "task-1")
	tracker.AddTask(contextID, "task-2")

	assert.Equal(t, contextID, tracker.GetContextForTask("task-1"))
	assert.Equal(t, contextID, tracker.GetContextForTask("task-2"))
	assert.Empty(t, tracker.GetContextForTask("task-nonexistent"))
}

func TestTaskTracker_GetAgentForContext(t *testing.T) {
	tracker := NewTaskTracker()
	agent1 := "http://agent1.com"
	agent2 := "http://agent2.com"

	tracker.RegisterContext(agent1, "context-1")
	tracker.RegisterContext(agent2, "context-2")

	assert.Equal(t, agent1, tracker.GetAgentForContext("context-1"))
	assert.Equal(t, agent2, tracker.GetAgentForContext("context-2"))
	assert.Empty(t, tracker.GetAgentForContext("context-nonexistent"))
}

func TestTaskTracker_HasContext(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"

	assert.False(t, tracker.HasContext("context-1"))

	tracker.RegisterContext(agentURL, "context-1")
	assert.True(t, tracker.HasContext("context-1"))
	assert.False(t, tracker.HasContext("context-2"))

	tracker.RegisterContext(agentURL, "context-2")
	assert.True(t, tracker.HasContext("context-1"))
	assert.True(t, tracker.HasContext("context-2"))
}

func TestTaskTracker_HasTask(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"
	contextID := "context-1"

	tracker.RegisterContext(agentURL, contextID)

	assert.False(t, tracker.HasTask("task-1"))

	tracker.AddTask(contextID, "task-1")
	assert.True(t, tracker.HasTask("task-1"))
	assert.False(t, tracker.HasTask("task-2"))

	tracker.AddTask(contextID, "task-2")
	assert.True(t, tracker.HasTask("task-1"))
	assert.True(t, tracker.HasTask("task-2"))
}

func TestTaskTracker_MultipleAgents(t *testing.T) {
	tracker := NewTaskTracker()
	agent1 := "http://agent1.com"
	agent2 := "http://agent2.com"

	// Register multiple contexts per agent
	tracker.RegisterContext(agent1, "context-agent1-1")
	tracker.RegisterContext(agent1, "context-agent1-2")
	tracker.RegisterContext(agent2, "context-agent2-1")

	assert.Equal(t, []string{"context-agent1-1", "context-agent1-2"}, tracker.GetContextsForAgent(agent1))
	assert.Equal(t, []string{"context-agent2-1"}, tracker.GetContextsForAgent(agent2))

	// Add tasks to different contexts
	tracker.AddTask("context-agent1-1", "task-1")
	tracker.AddTask("context-agent1-1", "task-2")
	tracker.AddTask("context-agent1-2", "task-3")
	tracker.AddTask("context-agent2-1", "task-4")

	assert.Equal(t, []string{"task-1", "task-2"}, tracker.GetTasksForContext("context-agent1-1"))
	assert.Equal(t, []string{"task-3"}, tracker.GetTasksForContext("context-agent1-2"))
	assert.Equal(t, []string{"task-4"}, tracker.GetTasksForContext("context-agent2-1"))
}

func TestTaskTracker_GetAllAgents(t *testing.T) {
	tracker := NewTaskTracker()

	agents := tracker.GetAllAgents()
	assert.Empty(t, agents)

	tracker.RegisterContext("http://agent-zulu.com", "context-1")
	tracker.RegisterContext("http://agent-alpha.com", "context-2")
	tracker.RegisterContext("http://agent-charlie.com", "context-3")

	agents = tracker.GetAllAgents()
	assert.Equal(t, []string{
		"http://agent-zulu.com",
		"http://agent-alpha.com",
		"http://agent-charlie.com",
	}, agents)

	tracker.RegisterContext("http://agent-alpha.com", "context-4")
	agents = tracker.GetAllAgents()
	assert.Equal(t, []string{
		"http://agent-zulu.com",
		"http://agent-alpha.com",
		"http://agent-charlie.com",
	}, agents)
}

func TestTaskTracker_GetAllContexts(t *testing.T) {
	tracker := NewTaskTracker()

	contexts := tracker.GetAllContexts()
	assert.Empty(t, contexts)

	tracker.RegisterContext("http://agent1.com", "context-zulu")
	tracker.RegisterContext("http://agent2.com", "context-alpha")
	tracker.RegisterContext("http://agent3.com", "context-charlie")

	contexts = tracker.GetAllContexts()

	assert.Len(t, contexts, 3)
	assert.Contains(t, contexts, "context-zulu")
	assert.Contains(t, contexts, "context-alpha")
	assert.Contains(t, contexts, "context-charlie")
}

func TestTaskTracker_ClearAllAgents(t *testing.T) {
	tracker := NewTaskTracker()

	tracker.RegisterContext("http://agent1.com", "context-1")
	tracker.RegisterContext("http://agent2.com", "context-2")
	tracker.AddTask("context-1", "task-1")
	tracker.AddTask("context-2", "task-2")

	tracker.ClearAllAgents()

	assert.Empty(t, tracker.GetTasksForContext("context-1"))
	assert.Empty(t, tracker.GetTasksForContext("context-2"))
	assert.Empty(t, tracker.GetContextsForAgent("http://agent1.com"))
	assert.Empty(t, tracker.GetContextsForAgent("http://agent2.com"))
	assert.Empty(t, tracker.GetAllAgents())
	assert.Empty(t, tracker.GetAllContexts())
}

func TestTaskTracker_PollingState(t *testing.T) {
	tracker := NewTaskTracker()

	assert.False(t, tracker.IsPolling("task-1"))
	assert.Nil(t, tracker.GetPollingState("task-1"))

	agentURL := "http://agent1.com"
	contextID := "context-1"
	tracker.RegisterContext(agentURL, contextID)

	state := &domain.TaskPollingState{
		TaskID:    "task-1",
		ContextID: contextID,
		AgentURL:  agentURL,
		StartedAt: time.Now(),
	}
	tracker.StartPolling("task-1", state)

	assert.True(t, tracker.IsPolling("task-1"))
	retrievedState := tracker.GetPollingState("task-1")
	assert.NotNil(t, retrievedState)
	assert.Equal(t, "task-1", retrievedState.TaskID)
	assert.Equal(t, agentURL, retrievedState.AgentURL)

	tracker.StopPolling("task-1")
	assert.False(t, tracker.IsPolling("task-1"))
	assert.Nil(t, tracker.GetPollingState("task-1"))
}

func TestTaskTracker_GetPollingTasksForContext(t *testing.T) {
	tracker := NewTaskTracker()
	agent1 := "http://agent1.com"
	context1 := "context-1"

	tracker.RegisterContext(agent1, context1)

	startTime := time.Now()

	state1 := &domain.TaskPollingState{TaskID: "task-1", ContextID: context1, AgentURL: agent1, StartedAt: startTime}
	state2 := &domain.TaskPollingState{TaskID: "task-2", ContextID: context1, AgentURL: agent1, StartedAt: startTime.Add(time.Second)}
	state3 := &domain.TaskPollingState{TaskID: "task-3", ContextID: context1, AgentURL: agent1, StartedAt: startTime.Add(2 * time.Second)}

	tracker.StartPolling("task-1", state1)
	tracker.StartPolling("task-2", state2)
	tracker.StartPolling("task-3", state3)

	tasks := tracker.GetPollingTasksForContext(context1)
	assert.Equal(t, []string{"task-1", "task-2", "task-3"}, tasks)

	tracker.StopPolling("task-2")
	tasks = tracker.GetPollingTasksForContext(context1)
	assert.Equal(t, []string{"task-1", "task-3"}, tasks)
}

func TestTaskTracker_GetAllPollingTasks(t *testing.T) {
	tracker := NewTaskTracker()

	tracker.RegisterContext("http://agent1.com", "context-1")
	tracker.RegisterContext("http://agent2.com", "context-2")

	startTime := time.Now()
	state1 := &domain.TaskPollingState{TaskID: "task-1", ContextID: "context-1", AgentURL: "http://agent1.com", StartedAt: startTime}
	state2 := &domain.TaskPollingState{TaskID: "task-2", ContextID: "context-2", AgentURL: "http://agent2.com", StartedAt: startTime.Add(time.Second)}
	state3 := &domain.TaskPollingState{TaskID: "task-3", ContextID: "context-1", AgentURL: "http://agent1.com", StartedAt: startTime.Add(2 * time.Second)}

	tracker.StartPolling("task-1", state1)
	tracker.StartPolling("task-2", state2)
	tracker.StartPolling("task-3", state3)

	tasks := tracker.GetAllPollingTasks()
	assert.Equal(t, []string{"task-1", "task-3", "task-2"}, tasks)
}

func TestTaskTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewTaskTracker()
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			tracker.RegisterContext("http://agent1.com", "context-123")
			tracker.AddTask("context-123", "task-123")
			tracker.GetTasksForContext("context-123")
			tracker.RemoveTask("task-123")
			tracker.GetContextsForAgent("http://agent1.com")
			tracker.ClearAllAgents()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			tracker.GetContextsForAgent("http://agent2.com")
			tracker.RegisterContext("http://agent2.com", "context-456")
			tracker.AddTask("context-456", "task-456")
			tracker.RemoveTask("task-456")
			tracker.GetTasksForContext("context-456")
			tracker.ClearAllAgents()
		}
		done <- true
	}()

	<-done
	<-done
}

func TestTaskTracker_GetAllPollingTasks_StableOrder(t *testing.T) {
	tracker := NewTaskTracker()

	tasks := []struct {
		id        string
		agentURL  string
		contextID string
		delay     time.Duration
	}{
		{"task-zulu", "http://agent-zulu.com", "context-zulu", 0},
		{"task-alpha", "http://agent-alpha.com", "context-alpha", 10 * time.Millisecond},
		{"task-charlie", "http://agent-charlie.com", "context-charlie", 20 * time.Millisecond},
		{"task-bravo", "http://agent-bravo.com", "context-bravo", 30 * time.Millisecond},
	}

	for _, task := range tasks {
		tracker.RegisterContext(task.agentURL, task.contextID)
	}

	startTime := time.Now()
	for _, task := range tasks {
		state := &domain.TaskPollingState{
			AgentURL:  task.agentURL,
			ContextID: task.contextID,
			TaskID:    task.id,
			StartedAt: startTime.Add(task.delay),
			IsPolling: true,
		}
		tracker.StartPolling(task.id, state)
	}

	var previousOrder []string
	for i := 0; i < 10; i++ {
		currentOrder := tracker.GetAllPollingTasks()

		if i == 0 {
			assert.Equal(t, []string{
				"task-zulu",
				"task-alpha",
				"task-charlie",
				"task-bravo",
			}, currentOrder, "tasks should be in FIFO order (agent → context → task)")
			previousOrder = currentOrder
		} else {
			assert.Equal(t, previousOrder, currentOrder, "order should be consistent across calls")
		}
	}
}

func TestTaskTracker_AddTaskWithoutContext(t *testing.T) {
	tracker := NewTaskTracker()

	tracker.AddTask("context-nonexistent", "task-1")

	assert.False(t, tracker.HasTask("task-1"))
	assert.Empty(t, tracker.GetTasksForContext("context-nonexistent"))
}

func TestTaskTracker_EmptyValues(t *testing.T) {
	tracker := NewTaskTracker()
	agentURL := "http://agent1.com"

	tracker.RegisterContext("", "context-1")
	tracker.RegisterContext(agentURL, "")
	assert.Empty(t, tracker.GetAllContexts())

	tracker.RegisterContext(agentURL, "context-1")
	tracker.AddTask("", "task-1")
	tracker.AddTask("context-1", "")
	assert.Empty(t, tracker.GetTasksForContext("context-1"))
}
