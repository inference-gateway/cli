package services

import (
	"sync"

	"github.com/inference-gateway/cli/internal/domain"
)

// TaskRetentionService manages in-memory retention of completed/terminal A2A tasks
type TaskRetentionService struct {
	tasks        []domain.TaskInfo
	maxRetention int
	mutex        sync.RWMutex
}

// Compile-time assertion that TaskRetentionService implements domain.TaskRetentionService interface
var _ domain.TaskRetentionService = (*TaskRetentionService)(nil)

// NewTaskRetentionService creates a new task retention service
func NewTaskRetentionService(maxRetention int) *TaskRetentionService {
	return &TaskRetentionService{
		tasks:        make([]domain.TaskInfo, 0, maxRetention),
		maxRetention: maxRetention,
	}
}

// AddTask adds a terminal task (completed, failed, canceled, etc.) to retention
func (t *TaskRetentionService) AddTask(task domain.TaskInfo) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.tasks = append([]domain.TaskInfo{task}, t.tasks...)

	if len(t.tasks) > t.maxRetention {
		t.tasks = t.tasks[:t.maxRetention]
	}
}

// GetTasks returns all retained tasks
func (t *TaskRetentionService) GetTasks() []domain.TaskInfo {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	result := make([]domain.TaskInfo, len(t.tasks))
	copy(result, t.tasks)
	return result
}

// Clear removes all retained tasks
func (t *TaskRetentionService) Clear() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.tasks = make([]domain.TaskInfo, 0, t.maxRetention)
}

// SetMaxRetention updates the maximum retention count
func (t *TaskRetentionService) SetMaxRetention(maxRetention int) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.maxRetention = maxRetention

	// Truncate if needed
	if len(t.tasks) > maxRetention {
		t.tasks = t.tasks[:maxRetention]
	}
}

// GetMaxRetention returns the current maximum retention count
func (t *TaskRetentionService) GetMaxRetention() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.maxRetention
}
