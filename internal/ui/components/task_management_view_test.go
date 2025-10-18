package components

import (
	"fmt"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestTaskRetentionService(t *testing.T) {
	service := &SimpleTaskRetentionService{
		completedTasks: make([]TaskInfo, 0),
		maxRetention:   3,
	}

	// Test adding tasks
	task1 := TaskInfo{
		TaskPollingState: domain.TaskPollingState{
			TaskID:    "task1",
			AgentURL:  "http://agent1",
			StartedAt: time.Now().Add(-5 * time.Minute),
		},
		AgentName: "agent1",
		Status:    "Completed",
		Completed: true,
	}

	task2 := TaskInfo{
		TaskPollingState: domain.TaskPollingState{
			TaskID:    "task2",
			AgentURL:  "http://agent2",
			StartedAt: time.Now().Add(-3 * time.Minute),
		},
		AgentName: "agent2",
		Status:    "Canceled",
		Canceled:  true,
	}

	task3 := TaskInfo{
		TaskPollingState: domain.TaskPollingState{
			TaskID:    "task3",
			AgentURL:  "http://agent3",
			StartedAt: time.Now().Add(-1 * time.Minute),
		},
		AgentName: "agent3",
		Status:    "Completed",
		Completed: true,
	}

	// Add tasks
	service.AddCompletedTask(task1)
	service.AddCompletedTask(task2)
	service.AddCompletedTask(task3)

	// Check that tasks are stored correctly
	completedTasks := service.GetCompletedTasks()
	if len(completedTasks) != 3 {
		t.Errorf("Expected 3 tasks, got %d", len(completedTasks))
	}

	// Check that most recent is first
	if completedTasks[0].TaskID != "task3" {
		t.Errorf("Expected most recent task first, got %s", completedTasks[0].TaskID)
	}

	// Test retention limit
	task4 := TaskInfo{
		TaskPollingState: domain.TaskPollingState{
			TaskID:    "task4",
			AgentURL:  "http://agent4",
			StartedAt: time.Now(),
		},
		AgentName: "agent4",
		Status:    "Completed",
		Completed: true,
	}

	service.AddCompletedTask(task4)
	completedTasks = service.GetCompletedTasks()

	// Should still have 3 tasks (retention limit)
	if len(completedTasks) != 3 {
		t.Errorf("Expected retention limit of 3, got %d tasks", len(completedTasks))
	}

	// The oldest task (task1) should be removed
	for _, task := range completedTasks {
		if task.TaskID == "task1" {
			t.Errorf("Expected oldest task to be removed due to retention limit")
		}
	}
}

func TestTaskRetentionServiceSetMaxRetention(t *testing.T) {
	service := &SimpleTaskRetentionService{
		completedTasks: make([]TaskInfo, 0),
		maxRetention:   5,
	}

	// Add some tasks
	for i := 0; i < 5; i++ {
		task := TaskInfo{
			TaskPollingState: domain.TaskPollingState{
				TaskID:   fmt.Sprintf("task%d", i),
				AgentURL: "http://agent",
			},
			AgentName: "agent",
			Status:    "Completed",
			Completed: true,
		}
		service.AddCompletedTask(task)
	}

	// Reduce retention to 2
	service.SetMaxRetention(2)

	// Should now only have 2 tasks
	if len(service.GetCompletedTasks()) != 2 {
		t.Errorf("Expected 2 tasks after reducing retention, got %d", len(service.GetCompletedTasks()))
	}

	// Check max retention getter
	if service.GetMaxRetention() != 2 {
		t.Errorf("Expected max retention of 2, got %d", service.GetMaxRetention())
	}
}

func TestTaskRetentionServiceClear(t *testing.T) {
	service := &SimpleTaskRetentionService{
		completedTasks: make([]TaskInfo, 0),
		maxRetention:   5,
	}

	// Add a task
	task := TaskInfo{
		TaskPollingState: domain.TaskPollingState{
			TaskID:   "task1",
			AgentURL: "http://agent",
		},
		AgentName: "agent",
		Status:    "Completed",
		Completed: true,
	}
	service.AddCompletedTask(task)

	// Clear all tasks
	service.ClearCompletedTasks()

	// Should have no tasks
	if len(service.GetCompletedTasks()) != 0 {
		t.Errorf("Expected 0 tasks after clear, got %d", len(service.GetCompletedTasks()))
	}
}
