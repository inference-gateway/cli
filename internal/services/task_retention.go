package services

import (
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/ui/components"
)

// TaskRetentionService manages completed and canceled tasks with retention
type TaskRetentionService struct {
	completedTasks []components.TaskInfo
	maxRetention   int
	mutex          sync.RWMutex
}

// NewTaskRetentionService creates a new task retention service
func NewTaskRetentionService(maxRetention int) *TaskRetentionService {
	if maxRetention <= 0 {
		maxRetention = 5 // Default retention
	}

	return &TaskRetentionService{
		completedTasks: make([]components.TaskInfo, 0, maxRetention),
		maxRetention:   maxRetention,
	}
}

// AddCompletedTask adds a completed or canceled task to the retention list
func (t *TaskRetentionService) AddCompletedTask(task components.TaskInfo) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Mark as completed/canceled with end time
	now := time.Now()
	task.ElapsedTime = now.Sub(task.StartedAt)

	// Add to the beginning of the list (most recent first)
	t.completedTasks = append([]components.TaskInfo{task}, t.completedTasks...)

	// Trim to max retention
	if len(t.completedTasks) > t.maxRetention {
		t.completedTasks = t.completedTasks[:t.maxRetention]
	}
}

// GetCompletedTasks returns all completed tasks
func (t *TaskRetentionService) GetCompletedTasks() []components.TaskInfo {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	// Return a copy to avoid race conditions
	result := make([]components.TaskInfo, len(t.completedTasks))
	copy(result, t.completedTasks)
	return result
}

// GetMaxRetention returns the maximum retention count
func (t *TaskRetentionService) GetMaxRetention() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.maxRetention
}

// SetMaxRetention updates the maximum retention count
func (t *TaskRetentionService) SetMaxRetention(maxRetention int) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if maxRetention <= 0 {
		maxRetention = 5
	}

	t.maxRetention = maxRetention

	// Trim existing tasks if new retention is smaller
	if len(t.completedTasks) > maxRetention {
		t.completedTasks = t.completedTasks[:maxRetention]
	}
}

// ClearCompletedTasks removes all completed tasks
func (t *TaskRetentionService) ClearCompletedTasks() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.completedTasks = make([]components.TaskInfo, 0, t.maxRetention)
}
