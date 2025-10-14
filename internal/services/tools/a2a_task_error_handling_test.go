package tools

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	utils "github.com/inference-gateway/cli/internal/utils"
	assert "github.com/stretchr/testify/assert"
	zap "go.uber.org/zap"
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

// MockA2AClient implements the A2AClient interface for testing
type MockA2AClient struct {
	sendTaskResponse *adk.JSONRPCSuccessResponse
	sendTaskError    error
	getTaskResponse  *adk.JSONRPCSuccessResponse
	getTaskError     error
}

func (m *MockA2AClient) SendTask(ctx context.Context, params adk.MessageSendParams) (*adk.JSONRPCSuccessResponse, error) {
	return m.sendTaskResponse, m.sendTaskError
}

func (m *MockA2AClient) GetTask(ctx context.Context, params adk.TaskQueryParams) (*adk.JSONRPCSuccessResponse, error) {
	return m.getTaskResponse, m.getTaskError
}

func (m *MockA2AClient) GetAgentCard(ctx context.Context) (*adk.AgentCard, error) { return nil, nil }
func (m *MockA2AClient) GetHealth(ctx context.Context) (*client.HealthResponse, error) {
	return nil, nil
}
func (m *MockA2AClient) SendTaskStreaming(ctx context.Context, params adk.MessageSendParams) (<-chan adk.JSONRPCSuccessResponse, error) {
	return nil, nil
}
func (m *MockA2AClient) ListTasks(ctx context.Context, params adk.TaskListParams) (*adk.JSONRPCSuccessResponse, error) {
	return nil, nil
}
func (m *MockA2AClient) CancelTask(ctx context.Context, params adk.TaskIdParams) (*adk.JSONRPCSuccessResponse, error) {
	return nil, nil
}
func (m *MockA2AClient) SetTimeout(timeout time.Duration)          {}
func (m *MockA2AClient) SetHTTPClient(client *http.Client)         {}
func (m *MockA2AClient) GetBaseURL() string                        { return "http://mock" }
func (m *MockA2AClient) SetLogger(logger *zap.Logger)              {}
func (m *MockA2AClient) GetLogger() *zap.Logger                    { return nil }
func (m *MockA2AClient) GetArtifactHelper() *client.ArtifactHelper { return nil }

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
		tracker := utils.NewSimpleTaskTracker()
		agentURL := "http://test-agent"
		tracker.SetTaskIDForAgent(agentURL, "nonexistent-task-123")

		mockClient := &MockA2AClient{
			sendTaskError: errors.New("A2A error: failed to resume task: task not found: nonexistent-task-123 (code: -32603)"),
		}

		tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

		assert.Equal(t, "nonexistent-task-123", tracker.GetTaskIDForAgent(agentURL))

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "Continue task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.False(t, result.Success)

		assert.Contains(t, result.Error, "Previous task no longer exists (cleared from tracker)")

		assert.Equal(t, "", tracker.GetTaskIDForAgent(agentURL))
	})

	t.Run("Execute clears tracker when task is completed", func(t *testing.T) {
		tracker := utils.NewSimpleTaskTracker()
		agentURL := "http://test-agent"
		tracker.SetTaskIDForAgent(agentURL, "completed-task-456")

		completedTask := adk.Task{
			ID: "completed-task-456",
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
		}

		mockClient := &MockA2AClient{
			sendTaskResponse: &adk.JSONRPCSuccessResponse{
				Result: completedTask,
			},
		}

		tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

		assert.Equal(t, "completed-task-456", tracker.GetTaskIDForAgent(agentURL))

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "Continue task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.False(t, result.Success)

		assert.Contains(t, result.Error, "is already completed (cleared from tracker)")

		assert.Equal(t, "", tracker.GetTaskIDForAgent(agentURL))
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
			existingTaskState:   adk.TaskStateCanceled,
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
			tracker := utils.NewSimpleTaskTracker()
			agentURL := "http://test-agent"
			tracker.SetTaskIDForAgent(agentURL, tt.existingTaskID)

			existingTask := adk.Task{
				ID: tt.existingTaskID,
				Status: adk.TaskStatus{
					State: tt.existingTaskState,
				},
			}

			newTask := adk.Task{
				ID: "new-task-999",
				Status: adk.TaskStatus{
					State: adk.TaskStateSubmitted,
				},
			}

			mockClient := &MockA2AClient{
				getTaskResponse: &adk.JSONRPCSuccessResponse{
					Result: existingTask,
				},
				getTaskError: tt.getTaskError,
				sendTaskResponse: &adk.JSONRPCSuccessResponse{
					Result: newTask,
				},
			}

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
				assert.Equal(t, tt.existingTaskID, tracker.GetTaskIDForAgent(agentURL))
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
				IdleTimeoutSec:    5,
			},
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}

	t.Run("allows submission to different agents independently", func(t *testing.T) {
		tracker := utils.NewSimpleTaskTracker()
		agentURL1 := "http://agent1.example.com"
		agentURL2 := "http://agent2.example.com"

		tracker.SetTaskIDForAgent(agentURL1, "working-task-agent1")

		workingTaskAgent1 := adk.Task{
			ID: "working-task-agent1",
			Status: adk.TaskStatus{
				State: adk.TaskStateWorking,
			},
		}

		newTaskAgent2 := adk.Task{
			ID: "new-task-agent2",
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
			ContextID: "context-agent2",
		}

		mockClient := &MockA2AClient{
			getTaskResponse: &adk.JSONRPCSuccessResponse{
				Result: workingTaskAgent1,
			},
			sendTaskResponse: &adk.JSONRPCSuccessResponse{
				Result: newTaskAgent2,
			},
			getTaskError: nil,
		}

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

		assert.Equal(t, "working-task-agent1", tracker.GetTaskIDForAgent(agentURL1))
		assert.Equal(t, "new-task-agent2", tracker.GetTaskIDForAgent(agentURL2))
		assert.Equal(t, "context-agent2", tracker.GetContextIDForAgent(agentURL2))
	})
}

func TestA2ASubmitTaskTool_NoExistingTask(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Task: config.A2ATaskConfig{
				StatusPollSeconds: 1,
				IdleTimeoutSec:    5,
			},
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}

	t.Run("creates new task when no existing task ID in tracker", func(t *testing.T) {
		tracker := utils.NewSimpleTaskTracker()
		agentURL := "http://test-agent"

		newTask := adk.Task{
			ID: "new-task-123",
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
			ContextID: "context-456",
		}

		mockClient := &MockA2AClient{
			sendTaskResponse: &adk.JSONRPCSuccessResponse{
				Result: newTask,
			},
			getTaskResponse: &adk.JSONRPCSuccessResponse{
				Result: newTask,
			},
		}

		tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

		assert.Equal(t, "", tracker.GetTaskIDForAgent(agentURL))

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "New task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.True(t, result.Success)

		assert.Equal(t, "new-task-123", tracker.GetTaskIDForAgent(agentURL))
		assert.Equal(t, "context-456", tracker.GetContextIDForAgent(agentURL))
	})
}
