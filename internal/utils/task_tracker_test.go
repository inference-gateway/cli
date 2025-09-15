package utils

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
)

func TestSimpleTaskTracker_SetAndGetTaskID(t *testing.T) {
	tests := []struct {
		name           string
		operations     []func(*SimpleTaskTracker)
		expectedTaskID string
	}{
		{
			name: "first task ID is stored",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("task-123") },
			},
			expectedTaskID: "task-123",
		},
		{
			name: "only first task ID is kept",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("task-123") },
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("task-456") },
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("task-789") },
			},
			expectedTaskID: "task-123",
		},
		{
			name: "empty task ID is ignored",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("") },
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("task-123") },
			},
			expectedTaskID: "task-123",
		},
		{
			name: "clear removes task ID",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("task-123") },
				func(tt *SimpleTaskTracker) { tt.ClearTaskID() },
			},
			expectedTaskID: "",
		},
		{
			name: "can set new ID after clearing",
			operations: []func(*SimpleTaskTracker){
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("task-123") },
				func(tt *SimpleTaskTracker) { tt.ClearTaskID() },
				func(tt *SimpleTaskTracker) { tt.SetFirstTaskID("task-456") },
			},
			expectedTaskID: "task-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewSimpleTaskTracker().(*SimpleTaskTracker)

			for _, op := range tt.operations {
				op(tracker)
			}

			assert.Equal(t, tt.expectedTaskID, tracker.GetFirstTaskID())
		})
	}
}

func TestSimpleTaskTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewSimpleTaskTracker()
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			tracker.SetFirstTaskID("task-123")
			tracker.GetFirstTaskID()
			tracker.ClearTaskID()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			tracker.GetFirstTaskID()
			tracker.SetFirstTaskID("task-456")
			tracker.ClearTaskID()
		}
		done <- true
	}()

	<-done
	<-done
}
