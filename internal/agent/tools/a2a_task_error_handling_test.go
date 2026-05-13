package tools

import (
	"context"
	"errors"
	"testing"

	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	adkmocks "github.com/inference-gateway/cli/tests/mocks/adk"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
	assert "github.com/stretchr/testify/assert"
)

func TestA2ASubmitTaskTool_isTaskNotFoundError(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}
	tool := NewA2ASubmitTaskTool(cfg, nil)

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "task not found error",
			err:      errors.New("task not found: 3d170a14-416d-4ea6-ba6f-e53ed1b561c2"),
			expected: true,
		},
		{
			name:     "generic not found error",
			err:      errors.New("resource not found"),
			expected: true,
		},
		{
			name:     "A2A error code 32603",
			err:      errors.New("A2A error: failed to resume task: task not found: 3d170a14-416d-4ea6-ba6f-e53ed1b561c2 (code: -32603)"),
			expected: true,
		},
		{
			name:     "case insensitive matching",
			err:      errors.New("TASK NOT FOUND"),
			expected: true,
		},
		{
			name:     "unrelated error",
			err:      errors.New("connection timeout"),
			expected: false,
		},
		{
			name:     "partial match should not trigger",
			err:      errors.New("task found successfully"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.isTaskNotFoundError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestA2ASubmitTaskTool_CompletedTaskHandling(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Task: config.A2ATaskConfig{
				StatusPollSeconds: 1,
			},
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}

	t.Run("Execute clears tracker when task not found", func(t *testing.T) {
		tracker := &mocks.FakeA2ATaskTracker{}
		agentURL := "http://test-agent"
		contextID := "context-123"
		taskID := "nonexistent-task-123"

		tracker.GetLatestContextForAgentReturns(contextID)
		tracker.GetLatestTaskForContextReturns(taskID)

		mockClient := &adkmocks.FakeA2AClient{}
		mockClient.SendTaskReturns(nil, errors.New("A2A error: failed to resume task: task not found: nonexistent-task-123 (code: -32603)"))

		tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "Continue task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.False(t, result.Success)

		assert.Contains(t, result.Error, "Previous task no longer exists (cleared from tracker)")

		// Verify RemoveTask was called
		assert.Equal(t, 1, tracker.RemoveTaskCallCount())
	})

	t.Run("Execute clears tracker when task is completed", func(t *testing.T) {
		tracker := &mocks.FakeA2ATaskTracker{}
		agentURL := "http://test-agent"
		contextID := "context-456"
		taskID := "completed-task-456"

		tracker.GetLatestContextForAgentReturns(contextID)
		tracker.GetLatestTaskForContextReturns(taskID)

		completedTask := adk.Task{
			ID:        taskID,
			ContextID: contextID,
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
		}

		mockClient := &adkmocks.FakeA2AClient{}
		mockClient.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: completedTask}, nil)

		tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "Continue task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.False(t, result.Success)

		assert.Contains(t, result.Error, "is already TASK_STATE_COMPLETED (cleared from tracker)")

		// Verify RemoveTask was called
		assert.Equal(t, 1, tracker.RemoveTaskCallCount())
	})
}

func TestA2ASubmitTaskTool_WorkingTaskGuardrail(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Task: config.A2ATaskConfig{
				StatusPollSeconds: 1,
			},
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}

	tests := []struct {
		name                 string
		existingTaskID       string
		existingTaskState    adk.TaskState
		getTaskError         error
		shouldPreventSubmit  bool
		expectedErrorMessage string
	}{
		{
			name:                 "prevents submission when task is in working state",
			existingTaskID:       "working-task-123",
			existingTaskState:    adk.TaskStateWorking,
			getTaskError:         nil,
			shouldPreventSubmit:  true,
			expectedErrorMessage: "existing task working-task-123 is still in working state",
		},
		{
			name:                "allows submission when task is completed",
			existingTaskID:      "completed-task-456",
			existingTaskState:   adk.TaskStateCompleted,
			getTaskError:        nil,
			shouldPreventSubmit: false,
		},
		{
			name:                "allows submission when task is failed",
			existingTaskID:      "failed-task-789",
			existingTaskState:   adk.TaskStateFailed,
			getTaskError:        nil,
			shouldPreventSubmit: false,
		},
		{
			name:                "allows submission when task is submitted",
			existingTaskID:      "submitted-task-101",
			existingTaskState:   adk.TaskStateSubmitted,
			getTaskError:        nil,
			shouldPreventSubmit: false,
		},
		{
			name:                "allows submission when task is canceled",
			existingTaskID:      "canceled-task-102",
			existingTaskState:   adk.TaskStateCancelled,
			getTaskError:        nil,
			shouldPreventSubmit: false,
		},
		{
			name:                "allows submission when task is rejected",
			existingTaskID:      "rejected-task-103",
			existingTaskState:   adk.TaskStateRejected,
			getTaskError:        nil,
			shouldPreventSubmit: false,
		},
		{
			name:                "allows submission when GetTask fails",
			existingTaskID:      "error-task-104",
			existingTaskState:   adk.TaskStateWorking,
			getTaskError:        errors.New("connection error"),
			shouldPreventSubmit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := &mocks.FakeA2ATaskTracker{}
			agentURL := "http://test-agent"
			contextID := "context-test"

			tracker.GetLatestContextForAgentReturns(contextID)
			tracker.GetLatestTaskForContextReturns(tt.existingTaskID)

			existingTask := adk.Task{
				ID:        tt.existingTaskID,
				ContextID: contextID,
				Status: adk.TaskStatus{
					State: tt.existingTaskState,
				},
			}

			newTask := adk.Task{
				ID:        "new-task-999",
				ContextID: contextID,
				Status: adk.TaskStatus{
					State: adk.TaskStateSubmitted,
				},
			}

			mockClient := &adkmocks.FakeA2AClient{}
			mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: existingTask}, tt.getTaskError)
			mockClient.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: newTask}, nil)

			tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

			args := map[string]any{
				"agent_url":        agentURL,
				"task_description": "New task description",
			}

			result, err := tool.Execute(context.Background(), args)

			assert.NoError(t, err)

			if tt.shouldPreventSubmit {
				assert.False(t, result.Success)
				assert.Contains(t, result.Error, tt.expectedErrorMessage)
				assert.Equal(t, 0, tracker.RemoveTaskCallCount())
			} else {
				if result.Success {
					assert.True(t, result.Success)
				}
			}
		})
	}
}

func TestA2ASubmitTaskTool_MultipleAgents(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Task: config.A2ATaskConfig{
				StatusPollSeconds: 1,
			},
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}

	t.Run("allows submission to different agents independently", func(t *testing.T) {
		tracker := &mocks.FakeA2ATaskTracker{}
		agentURL1 := "http://agent1.example.com"
		agentURL2 := "http://agent2.example.com"
		context1 := "context-agent1"
		context2 := "context-agent2"

		// Agent 1 has a context with a working task
		tracker.GetLatestContextForAgentReturnsOnCall(0, context1)
		tracker.GetLatestTaskForContextReturnsOnCall(0, "working-task-agent1")

		// Agent 2 has no context
		tracker.GetLatestContextForAgentReturnsOnCall(1, "")

		workingTaskAgent1 := adk.Task{
			ID:        "working-task-agent1",
			ContextID: context1,
			Status: adk.TaskStatus{
				State: adk.TaskStateWorking,
			},
		}

		newTaskAgent2 := adk.Task{
			ID:        "new-task-agent2",
			ContextID: context2,
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
		}

		mockClient := &adkmocks.FakeA2AClient{}
		mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: workingTaskAgent1}, nil)
		mockClient.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: newTaskAgent2}, nil)

		tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

		args1 := map[string]any{
			"agent_url":        agentURL1,
			"task_description": "Try to submit to agent 1",
		}

		result1, err := tool.Execute(context.Background(), args1)

		assert.NoError(t, err)
		assert.False(t, result1.Success)
		assert.Contains(t, result1.Error, "existing task working-task-agent1 is still in working state on agent http://agent1.example.com")

		args2 := map[string]any{
			"agent_url":        agentURL2,
			"task_description": "Submit to agent 2",
		}

		result2, err := tool.Execute(context.Background(), args2)

		assert.NoError(t, err)
		assert.True(t, result2.Success)

		// Verify new API methods were called
		assert.GreaterOrEqual(t, tracker.AddTaskCallCount(), 1)
		assert.GreaterOrEqual(t, tracker.RegisterContextCallCount(), 1)
	})
}

func TestA2ASubmitTaskTool_NoExistingTask(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Task: config.A2ATaskConfig{
				StatusPollSeconds: 1,
			},
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}

	t.Run("creates new task when no existing task ID in tracker", func(t *testing.T) {
		tracker := &mocks.FakeA2ATaskTracker{}
		agentURL := "http://test-agent"

		tracker.GetLatestContextForAgentReturns("")

		newTask := adk.Task{
			ID:        "new-task-123",
			ContextID: "context-456",
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
		}

		mockClient := &adkmocks.FakeA2AClient{}
		mockClient.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: newTask}, nil)
		mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: newTask}, nil)

		tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "New task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.True(t, result.Success)

		assert.Equal(t, 1, tracker.AddTaskCallCount())
		assert.Equal(t, 1, tracker.RegisterContextCallCount())
	})
}
