package a2a

import (
	"context"
	"errors"
	"net/http"
	"testing"

	assert "github.com/stretchr/testify/assert"

	a2amocks "github.com/inference-gateway/cli/tests/mocks/a2a"
	adkmocks "github.com/inference-gateway/cli/tests/mocks/adk"

	adk "github.com/inference-gateway/adk/types"

	config "github.com/inference-gateway/cli/config"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

func TestSubmitTaskTool_isTaskNotFoundError(t *testing.T) {
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
	tool := NewSubmitTaskTool(cfg, nil, nil, nil)

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

func TestSubmitTaskTool_CompletedTaskHandling(t *testing.T) {
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

	t.Run("Execute clears tracker when a resumed task is not found", func(t *testing.T) {
		tracker := &a2amocks.FakeTaskTracker{}
		agentURL := "http://test-agent"
		contextID := "context-123"
		taskID := "nonexistent-task-123"

		tracker.GetLatestContextForAgentReturns(contextID)
		tracker.GetLatestTaskForContextReturns(taskID)

		inputRequired := adk.Task{
			ID:        taskID,
			ContextID: &contextID,
			Status:    adk.TaskStatus{State: adk.TaskStateInputRequired},
		}

		mockClient := &adkmocks.FakeA2AClient{}
		mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: inputRequired}, nil)
		mockClient.SendTaskReturns(nil, errors.New("A2A error: failed to resume task: task not found: nonexistent-task-123 (code: -32603)"))

		tool := NewSubmitTaskToolWithClient(cfg, tracker, nil, nil, mockClient)

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "Continue task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.False(t, result.Success)

		assert.Contains(t, result.Error, "Previous task no longer exists (cleared from tracker)")

		assert.Equal(t, 1, tracker.RemoveTaskCallCount())
	})

	t.Run("Execute submits a fresh task after the previous one completed", func(t *testing.T) {
		tracker := &a2amocks.FakeTaskTracker{}
		agentURL := "http://test-agent"
		contextID := "context-456"
		taskID := "completed-task-456"

		tracker.GetLatestContextForAgentReturns(contextID)
		tracker.GetLatestTaskForContextReturns(taskID)

		completedTask := adk.Task{
			ID:        taskID,
			ContextID: &contextID,
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
		}

		mockClient := &adkmocks.FakeA2AClient{}
		mockClient.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: adk.SendMessageResponse{Task: &completedTask}}, nil)

		tool := NewSubmitTaskToolWithClient(cfg, tracker, nil, nil, mockClient)

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "Continue task",
		}

		result, err := tool.Execute(context.Background(), args)

		assert.NoError(t, err)
		assert.True(t, result.Success, result.Error)
		assert.Equal(t, 0, tracker.RemoveTaskCallCount())

		_, params := mockClient.SendTaskArgsForCall(0)
		assert.Nil(t, params.Message.ContextID)
		assert.Nil(t, params.Message.TaskID)
	})
}

func TestSubmitTaskTool_ContextReuse(t *testing.T) {
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
		name               string
		existingTaskState  adk.TaskState
		getTaskError       error
		requestedContextID string
		wantContextID      string
		wantResume         bool
	}{
		{
			name:              "working task on the agent does not block a new independent task",
			existingTaskState: adk.TaskStateWorking,
		},
		{
			name:              "completed task starts a fresh context",
			existingTaskState: adk.TaskStateCompleted,
		},
		{
			name:              "failed task starts a fresh context",
			existingTaskState: adk.TaskStateFailed,
		},
		{
			name:              "input-required task is resumed in its context",
			existingTaskState: adk.TaskStateInputRequired,
			wantContextID:     "context-test",
			wantResume:        true,
		},
		{
			name:              "GetTask failure starts a fresh context",
			existingTaskState: adk.TaskStateWorking,
			getTaskError:      errors.New("connection error"),
		},
		{
			name:               "explicit context_id continues that conversation",
			existingTaskState:  adk.TaskStateCompleted,
			requestedContextID: "context-test",
			wantContextID:      "context-test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := &a2amocks.FakeTaskTracker{}
			agentURL := "http://test-agent"
			contextID := "context-test"
			existingTaskID := "existing-task-123"

			tracker.GetLatestContextForAgentReturns(contextID)
			tracker.GetLatestTaskForContextReturns(existingTaskID)

			existingTask := adk.Task{
				ID:        existingTaskID,
				ContextID: &contextID,
				Status:    adk.TaskStatus{State: tt.existingTaskState},
			}
			newTask := adk.Task{
				ID:        "new-task-999",
				ContextID: ptrString("context-new"),
				Status:    adk.TaskStatus{State: adk.TaskStateSubmitted},
			}

			mockClient := &adkmocks.FakeA2AClient{}
			mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: existingTask}, tt.getTaskError)
			mockClient.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: adk.SendMessageResponse{Task: &newTask}}, nil)

			tool := NewSubmitTaskToolWithClient(cfg, tracker, nil, nil, mockClient)

			args := map[string]any{
				"agent_url":        agentURL,
				"task_description": "New task description",
			}
			if tt.requestedContextID != "" {
				args["context_id"] = tt.requestedContextID
			}

			result, err := tool.Execute(context.Background(), args)

			assert.NoError(t, err)
			assert.True(t, result.Success, result.Error)
			assert.Equal(t, 0, tracker.RemoveTaskCallCount())

			assert.Equal(t, 1, mockClient.SendTaskCallCount())
			_, params := mockClient.SendTaskArgsForCall(0)
			if tt.wantContextID == "" {
				assert.Nil(t, params.Message.ContextID, "independent task must not reuse the old context")
			} else {
				assert.NotNil(t, params.Message.ContextID)
				assert.Equal(t, tt.wantContextID, *params.Message.ContextID)
			}
			if tt.wantResume {
				assert.NotNil(t, params.Message.TaskID)
				assert.Equal(t, existingTaskID, *params.Message.TaskID)
			} else {
				assert.Nil(t, params.Message.TaskID)
			}
		})
	}
}

func TestSubmitTaskTool_MultipleAgents(t *testing.T) {
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

	t.Run("allows submission to different agents while another agent has a working task", func(t *testing.T) {
		tracker := &a2amocks.FakeTaskTracker{}
		agentURL1 := "http://agent1.example.com"
		agentURL2 := "http://agent2.example.com"
		context1 := "context-agent1"
		context2 := "context-agent2"

		tracker.GetLatestContextForAgentReturnsOnCall(0, context1)
		tracker.GetLatestTaskForContextReturnsOnCall(0, "working-task-agent1")

		tracker.GetLatestContextForAgentReturnsOnCall(1, "")

		workingTaskAgent1 := adk.Task{
			ID:        "working-task-agent1",
			ContextID: &context1,
			Status: adk.TaskStatus{
				State: adk.TaskStateWorking,
			},
		}

		newTaskAgent2 := adk.Task{
			ID:        "new-task-agent2",
			ContextID: &context2,
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
		}

		mockClient := &adkmocks.FakeA2AClient{}
		mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: workingTaskAgent1}, nil)
		mockClient.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: adk.SendMessageResponse{Task: &newTaskAgent2}}, nil)

		tool := NewSubmitTaskToolWithClient(cfg, tracker, nil, nil, mockClient)

		args1 := map[string]any{
			"agent_url":        agentURL1,
			"task_description": "Try to submit to agent 1",
		}

		result1, err := tool.Execute(context.Background(), args1)

		assert.NoError(t, err)
		assert.True(t, result1.Success, result1.Error)

		args2 := map[string]any{
			"agent_url":        agentURL2,
			"task_description": "Submit to agent 2",
		}

		result2, err := tool.Execute(context.Background(), args2)

		assert.NoError(t, err)
		assert.True(t, result2.Success)

		assert.GreaterOrEqual(t, tracker.AddTaskCallCount(), 1)
		assert.GreaterOrEqual(t, tracker.RegisterContextCallCount(), 1)
	})
}

func TestSubmitTaskTool_NoExistingTask(t *testing.T) {
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
		tracker := &a2amocks.FakeTaskTracker{}
		agentURL := "http://test-agent"

		tracker.GetLatestContextForAgentReturns("")

		newTask := adk.Task{
			ID:        "new-task-123",
			ContextID: ptrString("context-456"),
			Status: adk.TaskStatus{
				State: adk.TaskStateCompleted,
			},
		}

		mockClient := &adkmocks.FakeA2AClient{}
		mockClient.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: adk.SendMessageResponse{Task: &newTask}}, nil)
		mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: newTask}, nil)

		tool := NewSubmitTaskToolWithClient(cfg, tracker, nil, nil, mockClient)

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

func authTestConfig() *config.Config {
	return &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools:   config.A2AToolsConfig{SubmitTask: config.SubmitTaskToolConfig{Enabled: true}},
			Task:    config.A2ATaskConfig{StatusPollSeconds: 1},
		},
	}
}

func TestSubmitTaskTool_RejectedSubmissionIsAnAuthFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	client := &adkmocks.FakeA2AClient{}
	client.SendTaskReturns(nil, &adk.HTTPStatusError{StatusCode: http.StatusUnauthorized, Body: `{"error":"invalid token"}`})
	tool := NewSubmitTaskToolWithClient(authTestConfig(), nil, nil, nil, client)

	result, err := tool.Execute(t.Context(), map[string]any{
		"agent_url":        "https://research.example.com",
		"task_description": "summarise",
	})

	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, `Authentication failed for A2A agent "research.example.com"`)
	assert.NotContains(t, result.Error, "invalid token")
}

// TestSubmitTaskTool_PollingStopsOnAuthFailure: a rejected credential is
// rejected on every poll, so the job fails instead of polling forever.
func TestSubmitTaskTool_PollingStopsOnAuthFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	client := &adkmocks.FakeA2AClient{}
	client.GetTaskReturns(nil, &adk.HTTPStatusError{StatusCode: http.StatusForbidden, Body: "forbidden"})
	tool := NewSubmitTaskToolWithClient(authTestConfig(), nil, nil, nil, client)
	state := &a2adomain.TaskPollingState{TaskID: "t1", ContextID: "ctx1", AgentURL: "https://research.example.com"}

	result := tool.runA2APolling(t.Context(), state.AgentURL, "t1", state, func(scheddomain.JobSignal) {}, func(adk.Task) {})

	assert.False(t, result.Success)
	assert.Contains(t, result.Error, `Authentication failed for A2A agent "research.example.com"`)
	assert.Equal(t, 1, client.GetTaskCallCount())
}
