package a2a

import (
	"sync"

	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
)

// TaskRetentionService manages in-memory retention of completed/terminal A2A tasks
type TaskRetentionService struct {
	tasks        []a2adomain.TaskInfo
	maxRetention int
	mutex        sync.RWMutex
}

// Compile-time assertion that TaskRetentionService implements a2adomain.TaskRetentionService interface
var _ a2adomain.TaskRetentionService = (*TaskRetentionService)(nil)

// NewTaskRetentionService creates a new task retention service
func NewTaskRetentionService(maxRetention int) *TaskRetentionService {
	return &TaskRetentionService{
		tasks:        make([]a2adomain.TaskInfo, 0, maxRetention),
		maxRetention: maxRetention,
	}
}

// AddTask adds a terminal task (completed, failed, canceled, etc.) to retention
func (t *TaskRetentionService) AddTask(task a2adomain.TaskInfo) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.tasks = append([]a2adomain.TaskInfo{task}, t.tasks...)

	if len(t.tasks) > t.maxRetention {
		t.tasks = t.tasks[:t.maxRetention]
	}
}

// GetTasks returns all retained tasks
func (t *TaskRetentionService) GetTasks() []a2adomain.TaskInfo {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	result := make([]a2adomain.TaskInfo, len(t.tasks))
	copy(result, t.tasks)
	return result
}

// Clear removes all retained tasks
func (t *TaskRetentionService) Clear() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.tasks = make([]a2adomain.TaskInfo, 0, t.maxRetention)
}

// SetMaxRetention updates the maximum retention count
func (t *TaskRetentionService) SetMaxRetention(maxRetention int) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.maxRetention = maxRetention

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
