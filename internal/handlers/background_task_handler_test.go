package handlers

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	mocks "github.com/inference-gateway/cli/tests/mocks/generated"
)

func TestBackgroundTaskHandler_HandleBackgroundTaskToggle(t *testing.T) {
	tests := []struct {
		name                  string
		backgroundTaskManager domain.BackgroundTaskManager
		setupMocks            func(*mocks.FakeBackgroundTaskManager)
		expectError           bool
		expectStatusMessage   bool
	}{
		{
			name:                  "no background task manager",
			backgroundTaskManager: nil,
			expectError:           true,
		},
		{
			name:                  "no tasks",
			backgroundTaskManager: &mocks.FakeBackgroundTaskManager{},
			setupMocks: func(m *mocks.FakeBackgroundTaskManager) {
				m.GetActiveTasksReturns([]*domain.BackgroundTask{})
				m.GetAllTasksReturns([]*domain.BackgroundTask{})
			},
			expectStatusMessage: true,
		},
		{
			name:                  "active tasks",
			backgroundTaskManager: &mocks.FakeBackgroundTaskManager{},
			setupMocks: func(m *mocks.FakeBackgroundTaskManager) {
				tasks := []*domain.BackgroundTask{
					{
						ID:          "task1",
						Description: "Test task 1",
						Status:      domain.BackgroundTaskStatusRunning,
						StartTime:   time.Now().Add(-30 * time.Second),
					},
				}
				m.GetActiveTasksReturns(tasks)
				m.GetAllTasksReturns(tasks)
			},
			expectStatusMessage: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewBackgroundTaskHandler(tt.backgroundTaskManager)

			if mock, ok := tt.backgroundTaskManager.(*mocks.FakeBackgroundTaskManager); ok && tt.setupMocks != nil {
				tt.setupMocks(mock)
			}

			cmd := handler.HandleBackgroundTaskToggle()

			if cmd == nil {
				t.Error("expected command but got nil")
				return
			}

			msg := cmd()

			if tt.expectError {
				if errorEvent, ok := msg.(domain.ShowErrorEvent); ok {
					if errorEvent.Error == "" {
						t.Error("expected error message but got empty string")
					}
				} else {
					t.Errorf("expected ShowErrorEvent but got %T", msg)
				}
			} else if tt.expectStatusMessage {
				if statusEvent, ok := msg.(domain.SetStatusEvent); ok {
					if statusEvent.Message == "" {
						t.Error("expected status message but got empty string")
					}
				} else {
					t.Errorf("expected SetStatusEvent but got %T", msg)
				}
			}
		})
	}
}

func TestBackgroundTaskHandler_HandleBackgroundTaskStarted(t *testing.T) {
	tests := []struct {
		name  string
		event domain.BackgroundTaskStartedEvent
	}{
		{
			name: "valid started event",
			event: domain.BackgroundTaskStartedEvent{
				TaskID:      "task1",
				AgentURL:    "http://localhost:8080",
				Description: "Test task",
				Timestamp:   time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewBackgroundTaskHandler(nil)

			cmd := handler.HandleBackgroundTaskStarted(tt.event)

			if cmd == nil {
				t.Error("expected command but got nil")
				return
			}

			msg := cmd()

			if statusEvent, ok := msg.(domain.SetStatusEvent); ok {
				if statusEvent.Message == "" {
					t.Error("expected status message but got empty string")
				}
			} else {
				t.Errorf("expected SetStatusEvent but got %T", msg)
			}
		})
	}
}

func TestBackgroundTaskHandler_HandleBackgroundTaskCompleted(t *testing.T) {
	tests := []struct {
		name        string
		event       domain.BackgroundTaskCompletedEvent
		expectError bool
	}{
		{
			name: "successful completion",
			event: domain.BackgroundTaskCompletedEvent{
				TaskID:    "task1",
				Success:   true,
				Result:    "Task completed successfully",
				Timestamp: time.Now(),
			},
			expectError: false,
		},
		{
			name: "failed completion",
			event: domain.BackgroundTaskCompletedEvent{
				TaskID:    "task1",
				Success:   false,
				Error:     "Task failed with error",
				Timestamp: time.Now(),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewBackgroundTaskHandler(nil)

			cmd := handler.HandleBackgroundTaskCompleted(tt.event)

			if cmd == nil {
				t.Error("expected command but got nil")
				return
			}

			msg := cmd()

			if tt.expectError {
				if errorEvent, ok := msg.(domain.ShowErrorEvent); ok {
					if errorEvent.Error == "" {
						t.Error("expected error message but got empty string")
					}
				} else {
					t.Errorf("expected ShowErrorEvent but got %T", msg)
				}
			} else {
				if statusEvent, ok := msg.(domain.SetStatusEvent); ok {
					if statusEvent.Message == "" {
						t.Error("expected status message but got empty string")
					}
				} else {
					t.Errorf("expected SetStatusEvent but got %T", msg)
				}
			}
		})
	}
}

func TestBackgroundTaskHandler_UpdateBackgroundTaskCount(t *testing.T) {
	tests := []struct {
		name                  string
		backgroundTaskManager domain.BackgroundTaskManager
		setupMocks            func(*mocks.FakeBackgroundTaskManager)
		expectedCount         int
	}{
		{
			name:                  "no background task manager",
			backgroundTaskManager: nil,
		},
		{
			name:                  "no active tasks",
			backgroundTaskManager: &mocks.FakeBackgroundTaskManager{},
			setupMocks: func(m *mocks.FakeBackgroundTaskManager) {
				m.GetActiveTaskCountReturns(0)
			},
			expectedCount: 0,
		},
		{
			name:                  "multiple active tasks",
			backgroundTaskManager: &mocks.FakeBackgroundTaskManager{},
			setupMocks: func(m *mocks.FakeBackgroundTaskManager) {
				m.GetActiveTaskCountReturns(3)
			},
			expectedCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewBackgroundTaskHandler(tt.backgroundTaskManager)

			if mock, ok := tt.backgroundTaskManager.(*mocks.FakeBackgroundTaskManager); ok && tt.setupMocks != nil {
				tt.setupMocks(mock)
			}

			cmd := handler.UpdateBackgroundTaskCount()

			if tt.backgroundTaskManager == nil {
				if cmd != nil {
					t.Error("expected nil command when no background task manager")
				}
				return
			}

			if cmd == nil {
				t.Error("expected command but got nil")
				return
			}

			msg := cmd()

			if countEvent, ok := msg.(domain.BackgroundTaskCountUpdateEvent); ok {
				if countEvent.Count != tt.expectedCount {
					t.Errorf("expected count %d but got %d", tt.expectedCount, countEvent.Count)
				}
			} else {
				t.Errorf("expected BackgroundTaskCountUpdateEvent but got %T", msg)
			}
		})
	}
}
