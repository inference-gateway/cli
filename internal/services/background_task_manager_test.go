package services

import (
	"context"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func TestBackgroundTaskManager_SubmitTask(t *testing.T) {
	tests := []struct {
		name        string
		config      *config.Config
		agentURL    string
		description string
		args        map[string]any
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid task submission",
			config: &config.Config{
				Tools: config.ToolsConfig{
					Task: config.TaskToolConfig{
						Enabled: true,
					},
				},
			},
			agentURL:    "http://localhost:8080",
			description: "test task",
			args:        map[string]any{"test": "value"},
			expectError: false,
		},
		{
			name: "task submission with disabled tools",
			config: &config.Config{
				Tools: config.ToolsConfig{
					Task: config.TaskToolConfig{
						Enabled: false,
					},
				},
			},
			agentURL:    "http://localhost:8080",
			description: "test task",
			args:        map[string]any{"test": "value"},
			expectError: true,
			errorMsg:    "A2A tasks are disabled in configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskTracker := utils.NewSimpleTaskTracker()
			manager := NewBackgroundTaskManager(tt.config, taskTracker)

			ctx := context.Background()
			task, err := manager.SubmitTask(ctx, tt.agentURL, tt.description, tt.args)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("expected error %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if task == nil {
				t.Error("expected task but got nil")
				return
			}

			if task.AgentURL != tt.agentURL {
				t.Errorf("expected agent URL %q, got %q", tt.agentURL, task.AgentURL)
			}

			if task.Description != tt.description {
				t.Errorf("expected description %q, got %q", tt.description, task.Description)
			}

			if task.Status != domain.BackgroundTaskStatusPending {
				t.Errorf("expected status %q, got %q", domain.BackgroundTaskStatusPending, task.Status)
			}
		})
	}
}

func TestBackgroundTaskManager_GetActiveTasks(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*BackgroundTaskManager)
		expected int
	}{
		{
			name:     "no tasks",
			setup:    func(m *BackgroundTaskManager) {},
			expected: 0,
		},
		{
			name: "one active task",
			setup: func(m *BackgroundTaskManager) {
				task := &BackgroundTask{
					ID:     "test1",
					Status: BackgroundTaskStatusRunning,
				}
				m.tasks["test1"] = task
			},
			expected: 1,
		},
		{
			name: "mixed tasks",
			setup: func(m *BackgroundTaskManager) {
				m.tasks["test1"] = &BackgroundTask{
					ID:     "test1",
					Status: BackgroundTaskStatusRunning,
				}
				m.tasks["test2"] = &BackgroundTask{
					ID:     "test2",
					Status: BackgroundTaskStatusCompleted,
				}
				m.tasks["test3"] = &BackgroundTask{
					ID:     "test3",
					Status: BackgroundTaskStatusPending,
				}
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			taskTracker := utils.NewSimpleTaskTracker()
			manager := NewBackgroundTaskManager(cfg, taskTracker)

			tt.setup(manager)

			activeTasks := manager.GetActiveTasks()
			if len(activeTasks) != tt.expected {
				t.Errorf("expected %d active tasks, got %d", tt.expected, len(activeTasks))
			}
		})
	}
}

func TestBackgroundTaskManager_CancelTask(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*BackgroundTaskManager) string
		expectError bool
		errorMsg    string
	}{
		{
			name: "cancel existing task",
			setup: func(m *BackgroundTaskManager) string {
				ctx, cancel := context.WithCancel(context.Background())
				task := &BackgroundTask{
					ID:         "test1",
					Status:     BackgroundTaskStatusRunning,
					cancelFunc: cancel,
					cancelCtx:  ctx,
				}
				m.tasks["test1"] = task
				return "test1"
			},
			expectError: false,
		},
		{
			name: "cancel non-existent task",
			setup: func(m *BackgroundTaskManager) string {
				return "nonexistent"
			},
			expectError: true,
			errorMsg:    "task not found: nonexistent",
		},
		{
			name: "cancel completed task",
			setup: func(m *BackgroundTaskManager) string {
				task := &BackgroundTask{
					ID:     "test1",
					Status: BackgroundTaskStatusCompleted,
				}
				m.tasks["test1"] = task
				return "test1"
			},
			expectError: true,
			errorMsg:    "task cannot be cancelled in current status: completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			taskTracker := utils.NewSimpleTaskTracker()
			manager := NewBackgroundTaskManager(cfg, taskTracker)

			taskID := tt.setup(manager)

			err := manager.CancelTask(taskID)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("expected error %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			task, exists := manager.GetTask(taskID)
			if !exists {
				t.Error("task should still exist after cancellation")
				return
			}

			if task.Status != domain.BackgroundTaskStatusCancelled {
				t.Errorf("expected status %q, got %q", domain.BackgroundTaskStatusCancelled, task.Status)
			}
		})
	}
}

func TestBackgroundTaskManager_CleanupOldTasks(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*BackgroundTaskManager)
		maxAge   time.Duration
		expected int
	}{
		{
			name:     "no tasks to cleanup",
			setup:    func(m *BackgroundTaskManager) {},
			maxAge:   time.Hour,
			expected: 0,
		},
		{
			name: "cleanup old completed task",
			setup: func(m *BackgroundTaskManager) {
				oldTime := time.Now().Add(-2 * time.Hour)
				m.tasks["old"] = &BackgroundTask{
					ID:             "old",
					Status:         BackgroundTaskStatusCompleted,
					CompletionTime: &oldTime,
				}
				m.tasks["new"] = &BackgroundTask{
					ID:     "new",
					Status: BackgroundTaskStatusRunning,
				}
			},
			maxAge:   time.Hour,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			taskTracker := utils.NewSimpleTaskTracker()
			manager := NewBackgroundTaskManager(cfg, taskTracker)

			tt.setup(manager)

			cleaned := manager.CleanupOldTasks(tt.maxAge)
			if cleaned != tt.expected {
				t.Errorf("expected %d cleaned tasks, got %d", tt.expected, cleaned)
			}
		})
	}
}
