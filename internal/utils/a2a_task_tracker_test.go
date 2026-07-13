package utils

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	assert "github.com/stretchr/testify/assert"
)

type a2aReg struct {
	agent   string
	context string
}

type a2aTaskAdd struct {
	context string
	task    string
}

type a2aPollSpec struct {
	task    string
	context string
	agent   string
	offset  time.Duration
}

// newTrackerWith builds a tracker pre-populated with the given registrations and tasks.
func newTrackerWith(t *testing.T, regs []a2aReg, adds []a2aTaskAdd) *A2ATaskTrackerImpl {
	t.Helper()
	tracker := NewA2ATaskTracker()
	for _, r := range regs {
		tracker.RegisterContext(r.agent, r.context)
	}
	for _, a := range adds {
		tracker.AddTask(a.context, a.task)
	}
	return tracker
}

// assertStringList asserts want against got, treating an empty want as Empty.
func assertStringList(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) == 0 {
		assert.Empty(t, got)
		return
	}
	assert.Equal(t, want, got)
}

func TestA2ATaskTracker_ContextQueries(t *testing.T) {
	tests := []struct {
		name            string
		regs            []a2aReg
		contextsByAgent map[string][]string
		latestByAgent   map[string]string
		hasContext      map[string]bool
		agentByContext  map[string]string
		allAgents       []string
		checkAllAgents  bool
		allContexts     []string
		checkAllCtxs    bool
	}{
		{
			name:            "no registrations",
			contextsByAgent: map[string][]string{"http://agent1.com": nil},
			latestByAgent:   map[string]string{"http://agent1.com": ""},
			hasContext:      map[string]bool{"context-1": false},
			checkAllAgents:  true,
			checkAllCtxs:    true,
		},
		{
			name:            "single context",
			regs:            []a2aReg{{"http://agent1.com", "context-1"}},
			contextsByAgent: map[string][]string{"http://agent1.com": {"context-1"}},
			latestByAgent:   map[string]string{"http://agent1.com": "context-1"},
			hasContext:      map[string]bool{"context-1": true, "context-2": false},
		},
		{
			name: "two contexts in order",
			regs: []a2aReg{
				{"http://agent1.com", "context-1"},
				{"http://agent1.com", "context-2"},
			},
			contextsByAgent: map[string][]string{"http://agent1.com": {"context-1", "context-2"}},
			latestByAgent:   map[string]string{"http://agent1.com": "context-2"},
			hasContext:      map[string]bool{"context-1": true, "context-2": true},
		},
		{
			name: "three contexts in order",
			regs: []a2aReg{
				{"http://agent1.com", "context-1"},
				{"http://agent1.com", "context-2"},
				{"http://agent1.com", "context-3"},
			},
			contextsByAgent: map[string][]string{"http://agent1.com": {"context-1", "context-2", "context-3"}},
			latestByAgent:   map[string]string{"http://agent1.com": "context-3"},
		},
		{
			name: "duplicate registration ignored",
			regs: []a2aReg{
				{"http://agent1.com", "context-1"},
				{"http://agent1.com", "context-2"},
				{"http://agent1.com", "context-3"},
				{"http://agent1.com", "context-2"},
			},
			contextsByAgent: map[string][]string{"http://agent1.com": {"context-1", "context-2", "context-3"}},
		},
		{
			name: "multiple agents",
			regs: []a2aReg{
				{"http://agent1.com", "context-agent1-1"},
				{"http://agent1.com", "context-agent1-2"},
				{"http://agent2.com", "context-agent2-1"},
			},
			contextsByAgent: map[string][]string{
				"http://agent1.com": {"context-agent1-1", "context-agent1-2"},
				"http://agent2.com": {"context-agent2-1"},
			},
		},
		{
			name: "agent lookup per context",
			regs: []a2aReg{
				{"http://agent1.com", "context-1"},
				{"http://agent2.com", "context-2"},
			},
			agentByContext: map[string]string{
				"context-1":           "http://agent1.com",
				"context-2":           "http://agent2.com",
				"context-nonexistent": "",
			},
		},
		{
			name: "all agents in insertion order",
			regs: []a2aReg{
				{"http://agent-zulu.com", "context-1"},
				{"http://agent-alpha.com", "context-2"},
				{"http://agent-charlie.com", "context-3"},
			},
			allAgents:      []string{"http://agent-zulu.com", "http://agent-alpha.com", "http://agent-charlie.com"},
			checkAllAgents: true,
			allContexts:    []string{"context-1", "context-2", "context-3"},
			checkAllCtxs:   true,
		},
		{
			name: "re-registration keeps agent order",
			regs: []a2aReg{
				{"http://agent-zulu.com", "context-1"},
				{"http://agent-alpha.com", "context-2"},
				{"http://agent-charlie.com", "context-3"},
				{"http://agent-alpha.com", "context-4"},
			},
			allAgents:      []string{"http://agent-zulu.com", "http://agent-alpha.com", "http://agent-charlie.com"},
			checkAllAgents: true,
		},
		{
			name: "empty agent or context ignored",
			regs: []a2aReg{
				{"", "context-1"},
				{"http://agent1.com", ""},
			},
			checkAllCtxs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTrackerWith(t, tt.regs, nil)

			for agent, want := range tt.contextsByAgent {
				assertStringList(t, want, tracker.GetContextsForAgent(agent))
			}
			for agent, want := range tt.latestByAgent {
				assert.Equal(t, want, tracker.GetLatestContextForAgent(agent))
			}
			for contextID, want := range tt.hasContext {
				assert.Equal(t, want, tracker.HasContext(contextID))
			}
			for contextID, want := range tt.agentByContext {
				assert.Equal(t, want, tracker.GetAgentForContext(contextID))
			}
			if tt.checkAllAgents {
				assertStringList(t, tt.allAgents, tracker.GetAllAgents())
			}
			if tt.checkAllCtxs {
				got := tracker.GetAllContexts()
				assert.Len(t, got, len(tt.allContexts))
				for _, contextID := range tt.allContexts {
					assert.Contains(t, got, contextID)
				}
			}
		})
	}
}

func TestA2ATaskTracker_TaskQueries(t *testing.T) {
	reg1 := []a2aReg{{"http://agent1.com", "context-1"}}

	tests := []struct {
		name            string
		regs            []a2aReg
		adds            []a2aTaskAdd
		tasksByContext  map[string][]string
		latestByContext map[string]string
		hasTask         map[string]bool
		contextByTask   map[string]string
	}{
		{
			name:            "no tasks",
			regs:            reg1,
			tasksByContext:  map[string][]string{"context-1": nil},
			latestByContext: map[string]string{"context-1": ""},
			hasTask:         map[string]bool{"task-1": false},
		},
		{
			name:            "single task",
			regs:            reg1,
			adds:            []a2aTaskAdd{{"context-1", "task-1"}},
			tasksByContext:  map[string][]string{"context-1": {"task-1"}},
			latestByContext: map[string]string{"context-1": "task-1"},
			hasTask:         map[string]bool{"task-1": true, "task-2": false},
		},
		{
			name:            "two tasks in order",
			regs:            reg1,
			adds:            []a2aTaskAdd{{"context-1", "task-1"}, {"context-1", "task-2"}},
			tasksByContext:  map[string][]string{"context-1": {"task-1", "task-2"}},
			latestByContext: map[string]string{"context-1": "task-2"},
			hasTask:         map[string]bool{"task-1": true, "task-2": true},
			contextByTask: map[string]string{
				"task-1":           "context-1",
				"task-2":           "context-1",
				"task-nonexistent": "",
			},
		},
		{
			name:            "three tasks in order",
			regs:            reg1,
			adds:            []a2aTaskAdd{{"context-1", "task-1"}, {"context-1", "task-2"}, {"context-1", "task-3"}},
			tasksByContext:  map[string][]string{"context-1": {"task-1", "task-2", "task-3"}},
			latestByContext: map[string]string{"context-1": "task-3"},
		},
		{
			name: "duplicate task ignored",
			regs: reg1,
			adds: []a2aTaskAdd{
				{"context-1", "task-1"},
				{"context-1", "task-2"},
				{"context-1", "task-3"},
				{"context-1", "task-2"},
			},
			tasksByContext: map[string][]string{"context-1": {"task-1", "task-2", "task-3"}},
		},
		{
			name: "tasks across multiple contexts and agents",
			regs: []a2aReg{
				{"http://agent1.com", "context-agent1-1"},
				{"http://agent1.com", "context-agent1-2"},
				{"http://agent2.com", "context-agent2-1"},
			},
			adds: []a2aTaskAdd{
				{"context-agent1-1", "task-1"},
				{"context-agent1-1", "task-2"},
				{"context-agent1-2", "task-3"},
				{"context-agent2-1", "task-4"},
			},
			tasksByContext: map[string][]string{
				"context-agent1-1": {"task-1", "task-2"},
				"context-agent1-2": {"task-3"},
				"context-agent2-1": {"task-4"},
			},
		},
		{
			name:           "task without registered context ignored",
			adds:           []a2aTaskAdd{{"context-nonexistent", "task-1"}},
			tasksByContext: map[string][]string{"context-nonexistent": nil},
			hasTask:        map[string]bool{"task-1": false},
		},
		{
			name:           "empty context or task ignored",
			regs:           reg1,
			adds:           []a2aTaskAdd{{"", "task-1"}, {"context-1", ""}},
			tasksByContext: map[string][]string{"context-1": nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTrackerWith(t, tt.regs, tt.adds)

			for contextID, want := range tt.tasksByContext {
				assertStringList(t, want, tracker.GetTasksForContext(contextID))
			}
			for contextID, want := range tt.latestByContext {
				assert.Equal(t, want, tracker.GetLatestTaskForContext(contextID))
			}
			for taskID, want := range tt.hasTask {
				assert.Equal(t, want, tracker.HasTask(taskID))
			}
			for taskID, want := range tt.contextByTask {
				assert.Equal(t, want, tracker.GetContextForTask(taskID))
			}
		})
	}
}

func TestA2ATaskTracker_RemoveTask(t *testing.T) {
	tests := []struct {
		name   string
		remove []string
		want   []string
	}{
		{name: "remove middle", remove: []string{"task-2"}, want: []string{"task-1", "task-3"}},
		{name: "remove middle then first", remove: []string{"task-2", "task-1"}, want: []string{"task-3"}},
		{name: "remove all", remove: []string{"task-2", "task-1", "task-3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTrackerWith(t,
				[]a2aReg{{"http://agent1.com", "context-1"}},
				[]a2aTaskAdd{{"context-1", "task-1"}, {"context-1", "task-2"}, {"context-1", "task-3"}},
			)

			for _, taskID := range tt.remove {
				tracker.RemoveTask(taskID)
			}

			assertStringList(t, tt.want, tracker.GetTasksForContext("context-1"))
		})
	}
}

func TestA2ATaskTracker_RemoveContextAndClearAll(t *testing.T) {
	tests := []struct {
		name         string
		regs         []a2aReg
		adds         []a2aTaskAdd
		clear        func(tracker *A2ATaskTrackerImpl)
		checkAllGone bool
	}{
		{
			name: "remove context drops its tasks",
			regs: []a2aReg{{"http://agent1.com", "context-1"}},
			adds: []a2aTaskAdd{{"context-1", "task-1"}, {"context-1", "task-2"}},
			clear: func(tracker *A2ATaskTrackerImpl) {
				tracker.RemoveContext("context-1")
			},
		},
		{
			name: "clear all agents drops everything",
			regs: []a2aReg{
				{"http://agent1.com", "context-1"},
				{"http://agent2.com", "context-2"},
			},
			adds: []a2aTaskAdd{{"context-1", "task-1"}, {"context-2", "task-2"}},
			clear: func(tracker *A2ATaskTrackerImpl) {
				tracker.ClearAllAgents()
			},
			checkAllGone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTrackerWith(t, tt.regs, tt.adds)

			for _, r := range tt.regs {
				assert.True(t, tracker.HasContext(r.context))
			}
			for _, a := range tt.adds {
				assert.True(t, tracker.HasTask(a.task))
			}

			tt.clear(tracker)

			for _, r := range tt.regs {
				assert.False(t, tracker.HasContext(r.context))
				assert.Empty(t, tracker.GetContextsForAgent(r.agent))
			}
			for _, a := range tt.adds {
				assert.False(t, tracker.HasTask(a.task))
				assert.Empty(t, tracker.GetTasksForContext(a.context))
			}
			if tt.checkAllGone {
				assert.Empty(t, tracker.GetAllAgents())
				assert.Empty(t, tracker.GetAllContexts())
			}
		})
	}
}

func TestA2ATaskTracker_PollingState(t *testing.T) {
	tracker := NewA2ATaskTracker()

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

	retrievedState := tracker.GetPollingState("task-1")
	assert.NotNil(t, retrievedState)
	assert.Equal(t, "task-1", retrievedState.TaskID)
	assert.Equal(t, agentURL, retrievedState.AgentURL)

	tracker.StopPolling("task-1")
	assert.Nil(t, tracker.GetPollingState("task-1"))
}

func TestA2ATaskTracker_PollingTaskLists(t *testing.T) {
	tests := []struct {
		name         string
		specs        []a2aPollSpec
		stop         []string
		queryContext string
		want         []string
	}{
		{
			name: "tasks for context in start order",
			specs: []a2aPollSpec{
				{"task-1", "context-1", "http://agent1.com", 0},
				{"task-2", "context-1", "http://agent1.com", time.Second},
				{"task-3", "context-1", "http://agent1.com", 2 * time.Second},
			},
			queryContext: "context-1",
			want:         []string{"task-1", "task-2", "task-3"},
		},
		{
			name: "stopped task excluded from context",
			specs: []a2aPollSpec{
				{"task-1", "context-1", "http://agent1.com", 0},
				{"task-2", "context-1", "http://agent1.com", time.Second},
				{"task-3", "context-1", "http://agent1.com", 2 * time.Second},
			},
			stop:         []string{"task-2"},
			queryContext: "context-1",
			want:         []string{"task-1", "task-3"},
		},
		{
			name: "all polling tasks grouped by agent",
			specs: []a2aPollSpec{
				{"task-1", "context-1", "http://agent1.com", 0},
				{"task-2", "context-2", "http://agent2.com", time.Second},
				{"task-3", "context-1", "http://agent1.com", 2 * time.Second},
			},
			want: []string{"task-1", "task-3", "task-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewA2ATaskTracker()
			startTime := time.Now()

			for _, spec := range tt.specs {
				tracker.RegisterContext(spec.agent, spec.context)
				tracker.StartPolling(spec.task, &domain.TaskPollingState{
					TaskID:    spec.task,
					ContextID: spec.context,
					AgentURL:  spec.agent,
					StartedAt: startTime.Add(spec.offset),
				})
			}
			for _, taskID := range tt.stop {
				tracker.StopPolling(taskID)
			}

			var got []string
			if tt.queryContext != "" {
				got = tracker.GetPollingTasksForContext(tt.queryContext)
			} else {
				got = tracker.GetAllPollingTasks()
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestA2ATaskTracker_GetAllPollingTasks_StableOrder(t *testing.T) {
	tracker := NewA2ATaskTracker()

	tasks := []a2aPollSpec{
		{"task-zulu", "context-zulu", "http://agent-zulu.com", 0},
		{"task-alpha", "context-alpha", "http://agent-alpha.com", 10 * time.Millisecond},
		{"task-charlie", "context-charlie", "http://agent-charlie.com", 20 * time.Millisecond},
		{"task-bravo", "context-bravo", "http://agent-bravo.com", 30 * time.Millisecond},
	}

	for _, task := range tasks {
		tracker.RegisterContext(task.agent, task.context)
	}

	startTime := time.Now()
	for _, task := range tasks {
		state := &domain.TaskPollingState{
			AgentURL:  task.agent,
			ContextID: task.context,
			TaskID:    task.task,
			StartedAt: startTime.Add(task.offset),
			IsPolling: true,
		}
		tracker.StartPolling(task.task, state)
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

func TestA2ATaskTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewA2ATaskTracker()
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
