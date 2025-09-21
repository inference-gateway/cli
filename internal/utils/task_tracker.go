package utils

import (
	"sync"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// SimpleTaskTracker provides a simple in-memory implementation of TaskTracker
type SimpleTaskTracker struct {
	mu          sync.RWMutex
	firstTaskID string
	contextID   string
}

// NewSimpleTaskTracker creates a new SimpleTaskTracker
func NewSimpleTaskTracker() domain.TaskTracker {
	return &SimpleTaskTracker{}
}

// GetFirstTaskID returns the first task ID in the current session
func (t *SimpleTaskTracker) GetFirstTaskID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.firstTaskID
}

// SetFirstTaskID sets the first task ID if not already set
func (t *SimpleTaskTracker) SetFirstTaskID(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstTaskID == "" && taskID != "" {
		t.firstTaskID = taskID
	}
}

// ClearTaskID clears the stored task ID
func (t *SimpleTaskTracker) ClearTaskID() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.firstTaskID = ""
}

// GetContextID returns the context ID for the current session
func (t *SimpleTaskTracker) GetContextID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.contextID
}

// SetContextID sets the context ID if not already set
func (t *SimpleTaskTracker) SetContextID(contextID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.contextID == "" && contextID != "" {
		t.contextID = contextID
	}
}

// ClearContextID clears the stored context ID
func (t *SimpleTaskTracker) ClearContextID() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.contextID = ""
}
