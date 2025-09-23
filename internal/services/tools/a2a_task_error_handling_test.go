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
		tracker.SetFirstTaskID("nonexistent-task-123")

		mockClient := &MockA2AClient{
			sendTaskError: errors.New("A2A error: failed to resume task: task not found: nonexistent-task-123 (code: -32603)"),
		}

		tool := NewA2ASubmitTaskToolWithClient(cfg, tracker, mockClient)

		assert.Equal(t, "nonexistent-task-123", tracker.GetFirstTaskID())

		args := map[string]any{
			"agent_url":        "http://test-agent",
			"task_description": "Continue task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.False(t, result.Success)

		assert.Contains(t, result.Error, "Previous task no longer exists (cleared from tracker)")

		assert.Equal(t, "", tracker.GetFirstTaskID())
	})

	t.Run("Execute clears tracker when task is completed", func(t *testing.T) {
		tracker := utils.NewSimpleTaskTracker()
		tracker.SetFirstTaskID("completed-task-456")

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

		assert.Equal(t, "completed-task-456", tracker.GetFirstTaskID())

		args := map[string]any{
			"agent_url":        "http://test-agent",
			"task_description": "Continue task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.False(t, result.Success)

		assert.Contains(t, result.Error, "is already completed (cleared from tracker)")

		assert.Equal(t, "", tracker.GetFirstTaskID())
	})
}
