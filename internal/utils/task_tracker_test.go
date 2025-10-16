package utils

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	assert "github.com/stretchr/testify/assert"
)

func TestSimpleTaskTracker_SetAndGetTaskIDForAgent(t *testing.T) {
	tests := []struct {
		name           string
		operations     []func(*SimpleTaskTracker)
		agentURL       string
		expectedTaskID string
	}{
		{
			name: "task ID is stored for agent",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetTaskIDForAgent("http://agent1.com", "task-123") },
			},
			agentURL:       "http://agent1.com",
			expectedTaskID: "task-123",
		},
		{
			name: "task ID can be updated for agent",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetTaskIDForAgent("http://agent1.com", "task-123") },
				func(tt *SimpleTaskTracker) { tt.SetTaskIDForAgent("http://agent1.com", "task-456") },
			},
			agentURL:       "http://agent1.com",
			expectedTaskID: "task-456",
		},
		{
			name: "empty task ID is ignored",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetTaskIDForAgent("http://agent1.com", "task-123") },
				func(tt *SimpleTaskTracker) { tt.SetTaskIDForAgent("http://agent1.com", "") },
			},
			agentURL:       "http://agent1.com",
			expectedTaskID: "task-123",
		},
		{
			name: "clear removes task ID for specific agent",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetTaskIDForAgent("http://agent1.com", "task-123") },
				func(tt *SimpleTaskTracker) { tt.ClearTaskIDForAgent("http://agent1.com") },
			},
			agentURL:       "http://agent1.com",
			expectedTaskID: "",
		},
		{
			name: "can set new ID after clearing",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetTaskIDForAgent("http://agent1.com", "task-123") },
				func(tt *SimpleTaskTracker) { tt.ClearTaskIDForAgent("http://agent1.com") },
				func(tt *SimpleTaskTracker) { tt.SetTaskIDForAgent("http://agent1.com", "task-456") },
			},
			agentURL:       "http://agent1.com",
			expectedTaskID: "task-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewSimpleTaskTracker().(*SimpleTaskTracker)

			for _, op := range tt.operations {
				op(tracker)
			}

			assert.Equal(t, tt.expectedTaskID, tracker.GetTaskIDForAgent(tt.agentURL))
		})
	}
}

func TestSimpleTaskTracker_MultipleAgents(t *testing.T) {
	tracker := NewSimpleTaskTracker().(*SimpleTaskTracker)

	tracker.SetTaskIDForAgent("http://agent1.com", "task-agent1")
	tracker.SetTaskIDForAgent("http://agent2.com", "task-agent2")
	tracker.SetContextIDForAgent("http://agent1.com", "context-agent1")
	tracker.SetContextIDForAgent("http://agent2.com", "context-agent2")

	assert.Equal(t, "task-agent1", tracker.GetTaskIDForAgent("http://agent1.com"))
	assert.Equal(t, "task-agent2", tracker.GetTaskIDForAgent("http://agent2.com"))
	assert.Equal(t, "context-agent1", tracker.GetContextIDForAgent("http://agent1.com"))
	assert.Equal(t, "context-agent2", tracker.GetContextIDForAgent("http://agent2.com"))

	tracker.ClearTaskIDForAgent("http://agent1.com")
	assert.Equal(t, "", tracker.GetTaskIDForAgent("http://agent1.com"))
	assert.Equal(t, "task-agent2", tracker.GetTaskIDForAgent("http://agent2.com"))
}

func TestSimpleTaskTracker_SetAndGetContextIDForAgent(t *testing.T) {
	tests := []struct {
		name              string
		operations        []func(*SimpleTaskTracker)
		agentURL          string
		expectedContextID string
	}{
		{
			name: "context ID is stored for agent",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetContextIDForAgent("http://agent1.com", "context-123") },
			},
			agentURL:          "http://agent1.com",
			expectedContextID: "context-123",
		},
		{
			name: "context ID can be updated for agent",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetContextIDForAgent("http://agent1.com", "context-123") },
				func(tt *SimpleTaskTracker) { tt.SetContextIDForAgent("http://agent1.com", "context-456") },
			},
			agentURL:          "http://agent1.com",
			expectedContextID: "context-456",
		},
		{
			name: "empty context ID is ignored",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetContextIDForAgent("http://agent1.com", "context-123") },
				func(tt *SimpleTaskTracker) { tt.SetContextIDForAgent("http://agent1.com", "") },
			},
			agentURL:          "http://agent1.com",
			expectedContextID: "context-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewSimpleTaskTracker().(*SimpleTaskTracker)

			for _, op := range tt.operations {
				op(tracker)
			}

			assert.Equal(t, tt.expectedContextID, tracker.GetContextIDForAgent(tt.agentURL))
		})
	}
}

func TestSimpleTaskTracker_ClearAllAgents(t *testing.T) {
	tracker := NewSimpleTaskTracker().(*SimpleTaskTracker)

	tracker.SetTaskIDForAgent("http://agent1.com", "task-1")
	tracker.SetTaskIDForAgent("http://agent2.com", "task-2")
	tracker.SetContextIDForAgent("http://agent1.com", "context-1")
	tracker.SetContextIDForAgent("http://agent2.com", "context-2")

	tracker.ClearAllAgents()

	assert.Equal(t, "", tracker.GetTaskIDForAgent("http://agent1.com"))
	assert.Equal(t, "", tracker.GetTaskIDForAgent("http://agent2.com"))
	assert.Equal(t, "", tracker.GetContextIDForAgent("http://agent1.com"))
	assert.Equal(t, "", tracker.GetContextIDForAgent("http://agent2.com"))
}

func TestSimpleTaskTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewSimpleTaskTracker()
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			tracker.SetTaskIDForAgent("http://agent1.com", "task-123")
			tracker.GetTaskIDForAgent("http://agent1.com")
			tracker.ClearTaskIDForAgent("http://agent1.com")
			tracker.SetContextIDForAgent("http://agent1.com", "context-123")
			tracker.GetContextIDForAgent("http://agent1.com")
			tracker.ClearAllAgents()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			tracker.GetTaskIDForAgent("http://agent2.com")
			tracker.SetTaskIDForAgent("http://agent2.com", "task-456")
			tracker.ClearTaskIDForAgent("http://agent2.com")
			tracker.GetContextIDForAgent("http://agent2.com")
			tracker.SetContextIDForAgent("http://agent2.com", "context-456")
			tracker.ClearAllAgents()
		}
		done <- true
	}()

	<-done
	<-done
}

func TestSimpleTaskTracker_GetAllPollingAgents_StableOrder(t *testing.T) {
	tracker := NewSimpleTaskTracker().(*SimpleTaskTracker)

	agents := []string{
		"http://agent-zulu.com",
		"http://agent-alpha.com",
		"http://agent-charlie.com",
		"http://agent-bravo.com",
	}

	for _, agent := range agents {
		state := &domain.TaskPollingState{
			AgentURL:  agent,
			TaskID:    "task-123",
			StartedAt: time.Now(),
			IsPolling: true,
		}
		tracker.StartPolling(agent, state)
	}

	var previousOrder []string
	for i := 0; i < 10; i++ {
		currentOrder := tracker.GetAllPollingAgents()

		if i == 0 {
			assert.Equal(t, []string{
				"http://agent-alpha.com",
				"http://agent-bravo.com",
				"http://agent-charlie.com",
				"http://agent-zulu.com",
			}, currentOrder, "agents should be sorted alphabetically")
			previousOrder = currentOrder
		} else {
			assert.Equal(t, previousOrder, currentOrder, "order should be consistent across calls")
		}
	}
}
