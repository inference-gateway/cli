package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	adk "github.com/inference-gateway/adk/types"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestA2ASubmitTaskTool_Definition(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: true,
				},
			},
		},
		Prompts: *config.DefaultPromptsConfig(),
	}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	def := tool.Definition()

	assert.Equal(t, "A2A_SubmitTask", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "A2A agent")
	assert.Contains(t, *def.Function.Description, "delegate")
}

func TestA2ASubmitTaskTool_Execute_MissingAgentURL(t *testing.T) {
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
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	args := map[string]any{
		"task_description": "Test task",
	}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "agent_url parameter is required")
}

func TestA2ASubmitTaskTool_Execute_MissingTaskDescription(t *testing.T) {
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
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	args := map[string]any{
		"agent_url": "http://test-agent.example.com",
	}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "task_description parameter is required")
}

func TestA2ASubmitTaskTool_Validate(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid args",
			args: map[string]any{
				"agent_url":        "http://test-agent.example.com",
				"task_description": "Test task",
			},
			wantErr: false,
		},
		{
			name: "missing agent_url",
			args: map[string]any{
				"task_description": "Test task",
			},
			wantErr: true,
			errMsg:  "agent_url parameter is required",
		},
		{
			name: "missing task_description",
			args: map[string]any{
				"agent_url": "http://test-agent.example.com",
			},
			wantErr: true,
			errMsg:  "task_description parameter is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestA2ASubmitTaskTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name       string
		a2aEnabled bool
		expected   bool
	}{
		{
			name:       "disabled when A2A is disabled",
			a2aEnabled: false,
			expected:   false,
		},
		{
			name:       "enabled when A2A is enabled",
			a2aEnabled: true,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: tt.a2aEnabled,
					Tools: config.A2AToolsConfig{
						SubmitTask: config.SubmitTaskToolConfig{
							Enabled: true,
						},
					},
				},
			}
			tool := NewA2ASubmitTaskTool(cfg, nil, nil)

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestA2ASubmitTaskTool_FormatResult(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	taskResult := A2ASubmitTaskResult{
		TaskID:    "task-123",
		ContextID: "ctx-456",
		AgentURL:  "http://test-agent.example.com",
		State:     string(adk.TaskStateSubmitted),
		Success:   true,
		Message:   "Task submitted successfully",
	}

	result := &domain.ToolExecutionResult{
		ToolName: "A2A_SubmitTask",
		Success:  true,
		Data:     taskResult,
	}

	tests := []struct {
		name       string
		formatType domain.FormatterType
		contains   []string
	}{
		{
			name:       "LLM format",
			formatType: domain.FormatterLLM,
			contains:   []string{"Task()", "✓ Success", "Result:", "Task ID:", "task-123"},
		},
		{
			name:       "UI format",
			formatType: domain.FormatterUI,
			contains:   []string{"Task()", "✓", "Task submitted successfully"},
		},
		{
			name:       "Short format",
			formatType: domain.FormatterShort,
			contains:   []string{"Task submitted successfully"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := tool.FormatResult(result, tt.formatType)
			for _, expectedContent := range tt.contains {
				assert.Contains(t, formatted, expectedContent)
			}
		})
	}
}

func TestA2ASubmitTaskTool_FormatResult_IncludesUsageMetadata(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	metadata := adk.Struct{
		"usage": map[string]any{
			"prompt_tokens":     21695,
			"completion_tokens": 607,
			"total_tokens":      22302,
		},
		"execution_stats": map[string]any{
			"iterations":   5,
			"messages":     4,
			"tool_calls":   2,
			"failed_tools": 0,
		},
	}
	task := &adk.Task{
		ID:       "task-with-meta",
		Metadata: &metadata,
		Status:   adk.TaskStatus{State: adk.TaskStateCompleted},
	}
	result := &domain.ToolExecutionResult{
		ToolName: "A2A_SubmitTask",
		Success:  true,
		Data: A2ASubmitTaskResult{
			TaskID:   "task-with-meta",
			AgentURL: "http://browser-agent",
			State:    string(adk.TaskStateCompleted),
			Success:  true,
			Task:     task,
		},
	}

	out := tool.FormatResult(result, domain.FormatterLLM)
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "prompt_tokens=21695")
	assert.Contains(t, out, "completion_tokens=607")
	assert.Contains(t, out, "total_tokens=22302")
	assert.Contains(t, out, "Execution Stats:")
	assert.Contains(t, out, "tool_calls=2")
	assert.Contains(t, out, "failed_tools=0")
}

func TestA2ASubmitTaskTool_FormatResult_NoMetadataOmitsLines(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	result := &domain.ToolExecutionResult{
		ToolName: "A2A_SubmitTask",
		Success:  true,
		Data: A2ASubmitTaskResult{
			TaskID:   "no-meta",
			AgentURL: "http://old-agent",
			State:    string(adk.TaskStateCompleted),
			Success:  true,
			Task:     &adk.Task{ID: "no-meta", Status: adk.TaskStatus{State: adk.TaskStateCompleted}},
		},
	}

	out := tool.FormatResult(result, domain.FormatterLLM)
	assert.NotContains(t, out, "Usage:")
	assert.NotContains(t, out, "Execution Stats:")
}

func TestA2ASubmitTaskTool_FormatResult_FailedSurfacesError(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	errorText := "The `reasoning_content` in the thinking mode must be passed back to the API."

	tests := []struct {
		name string
		task adk.Task
	}{
		{
			name: "error in Status.Message text part",
			task: adk.Task{
				ID:        "task-failed-1",
				ContextID: "ctx-1",
				Status: adk.TaskStatus{
					State: adk.TaskStateFailed,
					Message: &adk.Message{
						MessageID: "err-1",
						Role:      adk.RoleAgent,
						Parts:     []adk.Part{{Text: ptrString(errorText)}},
					},
				},
			},
		},
		{
			name: "error only in Status.Message data part",
			task: adk.Task{
				ID:        "task-failed-2",
				ContextID: "ctx-2",
				Status: adk.TaskStatus{
					State: adk.TaskStateFailed,
					Message: &adk.Message{
						MessageID: "err-2",
						Role:      adk.RoleAgent,
						Parts: []adk.Part{{
							Data: &adk.DataPart{Data: adk.Struct{
								"status": "TASK_STATE_FAILED",
								"error":  errorText,
							}},
						}},
					},
				},
			},
		},
		{
			name: "error only in History (Status.Message nil)",
			task: adk.Task{
				ID:        "task-failed-3",
				ContextID: "ctx-3",
				Status:    adk.TaskStatus{State: adk.TaskStateFailed},
				History: []adk.Message{
					{
						MessageID: "u-1",
						Role:      adk.RoleUser,
						Parts:     []adk.Part{{Text: ptrString("do thing")}},
					},
					{
						MessageID: "a-1",
						Role:      adk.RoleAgent,
						Parts:     []adk.Part{{Text: ptrString(errorText)}},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			taskCopy := tc.task
			data := A2ASubmitTaskResult{
				TaskID:     taskCopy.ID,
				ContextID:  taskCopy.ContextID,
				AgentURL:   "http://browser-agent:8083",
				State:      string(taskCopy.Status.State),
				Success:    false,
				Message:    "Task TASK_STATE_FAILED: " + errorText,
				TaskResult: errorText,
				Task:       &taskCopy,
			}
			result := &domain.ToolExecutionResult{
				ToolName: "A2A_SubmitTask",
				Success:  false,
				Error:    errorText,
				Data:     data,
			}

			formatted := tool.FormatResult(result, domain.FormatterLLM)
			assert.Contains(t, formatted, "Failure reason:", "Failed task formatter must label the error")
			assert.Contains(t, formatted, errorText, "Failed task formatter must include the underlying error text")
		})
	}
}

func TestA2ASubmitTaskTool_FormatResult_FailedExtractsFromHistory(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	errorText := "DeepSeek returned 400: invalid_api_key"

	taskCopy := adk.Task{
		ID:        "task-failed-history",
		ContextID: "ctx",
		Status:    adk.TaskStatus{State: adk.TaskStateFailed},
		History: []adk.Message{
			{Role: adk.RoleUser, Parts: []adk.Part{{Text: ptrString("hi")}}},
			{Role: adk.RoleAgent, Parts: []adk.Part{{Text: ptrString(errorText)}}},
		},
	}

	data := A2ASubmitTaskResult{
		TaskID:    taskCopy.ID,
		ContextID: taskCopy.ContextID,
		AgentURL:  "http://browser-agent:8083",
		State:     string(taskCopy.Status.State),
		Success:   false,
		Message:   "Task TASK_STATE_FAILED",
		Task:      &taskCopy,
	}
	result := &domain.ToolExecutionResult{
		ToolName: "A2A_SubmitTask",
		Success:  false,
		Data:     data,
	}

	formatted := tool.FormatResult(result, domain.FormatterLLM)
	assert.Contains(t, formatted, "Failure reason:")
	assert.Contains(t, formatted, errorText)
}

func ptrString(s string) *string { return &s }

func TestA2ASubmitTaskTool_FormatPreview(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	taskResult := A2ASubmitTaskResult{
		State:   string(adk.TaskStateSubmitted),
		Success: true,
		Message: "Task submitted successfully",
	}

	result := &domain.ToolExecutionResult{
		ToolName: "A2A_SubmitTask",
		Success:  true,
		Data:     taskResult,
	}

	preview := tool.FormatPreview(result)
	assert.Contains(t, preview, "Task submitted successfully")
}

func TestA2ASubmitTaskTool_ShouldCollapseArg(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	assert.True(t, tool.ShouldCollapseArg("metadata"))
	assert.False(t, tool.ShouldCollapseArg("agent_url"))
	assert.False(t, tool.ShouldCollapseArg("task_description"))
}

func TestA2ASubmitTaskTool_ShouldAlwaysExpand(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	assert.False(t, tool.ShouldAlwaysExpand())
}

func TestArtifactDownloadURL_FallsBackToFilePartURI(t *testing.T) {
	uri := "http://localhost:8084/artifacts/ctx/art/fullpage.png"
	name := "fullpage.png"

	tests := []struct {
		name     string
		artifact adk.Artifact
		want     string
	}{
		{
			name: "metadata url wins",
			artifact: adk.Artifact{
				ArtifactID: "a1",
				Metadata:   &adk.Struct{"url": "http://meta-url"},
				Parts:      []adk.Part{{File: &adk.FilePart{FileWithURI: &uri, Name: name}}},
			},
			want: "http://meta-url",
		},
		{
			name: "file part uri fallback",
			artifact: adk.Artifact{
				ArtifactID: "a2",
				Parts:      []adk.Part{{File: &adk.FilePart{FileWithURI: &uri, Name: name}}},
			},
			want: uri,
		},
		{
			name:     "no url anywhere",
			artifact: adk.Artifact{ArtifactID: "a3"},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, artifactDownloadURL(tt.artifact))
		})
	}
}

func TestA2ASubmitTaskTool_FormatResult_ArtifactSavedTo(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)
	name := "shot.png"

	result := &domain.ToolExecutionResult{
		ToolName: "A2A_SubmitTask",
		Success:  true,
		Data: A2ASubmitTaskResult{
			TaskID:  "task-1",
			State:   string(adk.TaskStateCompleted),
			Success: true,
			Task: &adk.Task{
				ID: "task-1",
				Artifacts: []adk.Artifact{{
					ArtifactID: "a1",
					Name:       &name,
					Metadata: &adk.Struct{
						"url":        "http://localhost:8084/artifacts/shot.png",
						"local_path": "/home/user/.infer/tmp/shot.png",
					},
				}},
			},
		},
	}

	formatted := tool.FormatResult(result, domain.FormatterLLM)
	assert.Contains(t, formatted, "Download URL: http://localhost:8084/artifacts/shot.png")
	assert.Contains(t, formatted, "Saved to: /home/user/.infer/tmp/shot.png")
	assert.Contains(t, formatted, "already downloaded locally")
	assert.NotContains(t, formatted, "Use WebFetch tool")
}

func TestA2ASubmitTaskTool_DownloadArtifacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer server.Close()

	cfg := &config.Config{A2A: config.A2AConfig{Enabled: true, Agents: []string{server.URL}}}
	cfg.SetConfigDir(t.TempDir())
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	trustedURL := server.URL + "/artifacts/shot.png"
	task := &adk.Task{
		ID: "task-1",
		Artifacts: []adk.Artifact{
			{ArtifactID: "trusted", Metadata: &adk.Struct{"url": trustedURL}},
			{ArtifactID: "untrusted", Metadata: &adk.Struct{"url": "http://not-an-agent.example.com/x.png"}},
		},
	}

	tool.downloadArtifacts(task)

	savedPath := artifactLocalPath(task.Artifacts[0])
	require.NotEmpty(t, savedPath)
	content, err := os.ReadFile(savedPath)
	require.NoError(t, err)
	assert.Equal(t, "png-bytes", string(content))

	assert.Empty(t, artifactLocalPath(task.Artifacts[1]))
}

func TestA2ASubmitTaskTool_HandleTaskState_NoDownloadByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer server.Close()

	cfg := &config.Config{A2A: config.A2AConfig{Enabled: true, Agents: []string{server.URL}}}
	cfg.SetConfigDir(t.TempDir())
	tool := NewA2ASubmitTaskTool(cfg, nil, nil)

	task := adk.Task{
		ID:     "task-1",
		Status: adk.TaskStatus{State: adk.TaskStateCompleted},
		Artifacts: []adk.Artifact{
			{ArtifactID: "a1", Metadata: &adk.Struct{"url": server.URL + "/artifacts/shot.png"}},
		},
	}
	state := &domain.TaskPollingState{TaskID: "task-1", StartedAt: time.Now()}

	done, result := tool.handleTaskState(server.URL, "task-1", 1, state, task, "")
	require.True(t, done)
	require.NotNil(t, result)

	submit, ok := result.Data.(A2ASubmitTaskResult)
	require.True(t, ok)
	assert.Empty(t, artifactLocalPath(submit.Task.Artifacts[0]))
}
