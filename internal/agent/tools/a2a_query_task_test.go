package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestA2AQueryTaskTool_Definition(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryTask: config.QueryTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}
	tool := NewA2AQueryTaskTool(cfg, nil)
	def := tool.Definition()

	if def.Function.Name != "A2A_QueryTask" {
		t.Errorf("Expected function name to be 'A2A_QueryTask', got %s", def.Function.Name)
	}

	if def.Function.Description == nil {
		t.Error("Expected function description to be non-nil")
	}

	if def.Function.Parameters == nil {
		t.Error("Expected function parameters to be non-nil")
	}
}

func TestA2AQueryTaskTool_Validate(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			name: "valid arguments",
			args: map[string]any{
				"agent_url":  "http://example.com",
				"context_id": "ctx123",
				"task_id":    "task456",
			},
			wantErr: false,
		},
		{
			name: "missing agent_url",
			args: map[string]any{
				"context_id": "ctx123",
				"task_id":    "task456",
			},
			wantErr: true,
		},
		{
			name: "missing context_id",
			args: map[string]any{
				"agent_url": "http://example.com",
				"task_id":   "task456",
			},
			wantErr: true,
		},
		{
			name: "missing task_id",
			args: map[string]any{
				"agent_url":  "http://example.com",
				"context_id": "ctx123",
			},
			wantErr: true,
		},
		{
			name: "invalid agent_url type",
			args: map[string]any{
				"agent_url":  123,
				"context_id": "ctx123",
				"task_id":    "task456",
			},
			wantErr: true,
		},
		{
			name: "invalid context_id type",
			args: map[string]any{
				"agent_url":  "http://example.com",
				"context_id": 123,
				"task_id":    "task456",
			},
			wantErr: true,
		},
		{
			name: "invalid task_id type",
			args: map[string]any{
				"agent_url":  "http://example.com",
				"context_id": "ctx123",
				"task_id":    123,
			},
			wantErr: true,
		},
	}

	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryTask: config.QueryTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}
	tool := NewA2AQueryTaskTool(cfg, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestA2AQueryTaskTool_Execute_Disabled(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryTask: config.QueryTaskToolConfig{
					Enabled: false,
				},
			},
		},
	}
	tool := NewA2AQueryTaskTool(cfg, nil)

	args := map[string]any{
		"agent_url":  "http://example.com",
		"context_id": "ctx123",
		"task_id":    "task456",
	}

	result, err := tool.Execute(context.Background(), args)

	if err != nil {
		t.Errorf("Execute() returned unexpected error: %v", err)
	}

	if result.Success {
		t.Error("Expected Execute() to fail when tool is disabled")
	}

	if result.Error != "A2A connections are disabled in configuration" {
		t.Errorf("Expected specific error message, got: %s", result.Error)
	}
}

func TestA2AQueryTaskTool_Execute_InvalidArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]any
		wantErr  string
		wantFail bool
	}{
		{
			name: "missing agent_url",
			args: map[string]any{
				"context_id": "ctx123",
				"task_id":    "task456",
			},
			wantErr:  "agent_url parameter is required and must be a string",
			wantFail: true,
		},
		{
			name: "missing context_id",
			args: map[string]any{
				"agent_url": "http://example.com",
				"task_id":   "task456",
			},
			wantErr:  "context_id parameter is required and must be a string",
			wantFail: true,
		},
		{
			name: "missing task_id",
			args: map[string]any{
				"agent_url":  "http://example.com",
				"context_id": "ctx123",
			},
			wantErr:  "task_id parameter is required and must be a string",
			wantFail: true,
		},
	}

	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryTask: config.QueryTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}
	tool := NewA2AQueryTaskTool(cfg, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.args)

			if err != nil {
				t.Errorf("Execute() returned unexpected error: %v", err)
			}

			if result.Success == tt.wantFail {
				t.Errorf("Expected Success to be %v, got %v", !tt.wantFail, result.Success)
			}

			if result.Error != tt.wantErr {
				t.Errorf("Expected error %q, got %q", tt.wantErr, result.Error)
			}
		})
	}
}

func TestA2AQueryTaskTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name       string
		a2aEnabled bool
		want       bool
	}{
		{
			name:       "disabled when A2A is disabled",
			a2aEnabled: false,
			want:       false,
		},
		{
			name:       "enabled when A2A is enabled",
			a2aEnabled: true,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: tt.a2aEnabled,
					Tools: config.A2AToolsConfig{
						QueryTask: config.QueryTaskToolConfig{
							Enabled: true,
						},
					},
				},
			}
			tool := NewA2AQueryTaskTool(cfg, nil)

			if got := tool.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestA2AQueryTaskTool_FormatResult(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryTask: config.QueryTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}
	tool := NewA2AQueryTaskTool(cfg, nil)

	result := &domain.ToolExecutionResult{
		ToolName:  "A2A_QueryTask",
		Arguments: map[string]any{"agent_url": "http://example.com"},
		Success:   true,
		Duration:  time.Second,
		Data: A2AQueryTaskResult{
			AgentName: "http://example.com",
			ContextID: "ctx123",
			TaskID:    "task456",
			Task: &adk.Task{
				ID: "task456",
				Status: adk.TaskStatus{
					State: adk.TaskStateCompleted,
				},
			},
			Success: true,
			Message: "Task task456 is completed",
		},
	}

	tests := []struct {
		name       string
		formatType domain.FormatterType
	}{
		{
			name:       "UI format",
			formatType: domain.FormatterUI,
		},
		{
			name:       "LLM format",
			formatType: domain.FormatterLLM,
		},
		{
			name:       "Short format",
			formatType: domain.FormatterShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := tool.FormatResult(result, tt.formatType)
			if output == "" {
				t.Error("FormatResult() returned empty string")
			}
		})
	}
}

func TestA2AQueryTaskTool_FormatForLLM_FailedTaskSurfacesReason(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryTask: config.QueryTaskToolConfig{Enabled: true},
			},
		},
	}
	tool := NewA2AQueryTaskTool(cfg, nil)

	errorText := "DeepSeek: The `reasoning_content` in the thinking mode must be passed back to the API."
	errorTextPtr := errorText

	tests := []struct {
		name string
		task *adk.Task
	}{
		{
			name: "Status.Message TextPart",
			task: &adk.Task{
				ID: "t1",
				Status: adk.TaskStatus{
					State: adk.TaskStateFailed,
					Message: &adk.Message{
						MessageID: "m1",
						Role:      adk.RoleAgent,
						Parts:     []adk.Part{{Text: &errorTextPtr}},
					},
				},
			},
		},
		{
			name: "Status.Message DataPart with error key",
			task: &adk.Task{
				ID: "t2",
				Status: adk.TaskStatus{
					State: adk.TaskStateFailed,
					Message: &adk.Message{
						MessageID: "m2",
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
			name: "fallback to last agent History entry",
			task: &adk.Task{
				ID:     "t3",
				Status: adk.TaskStatus{State: adk.TaskStateFailed},
				History: []adk.Message{
					{Role: adk.RoleUser, Parts: []adk.Part{{Text: stringPtr("research")}}},
					{Role: adk.RoleAgent, Parts: []adk.Part{{Text: &errorTextPtr}}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := &domain.ToolExecutionResult{
				ToolName: "A2A_QueryTask",
				Success:  false,
				Data: A2AQueryTaskResult{
					AgentName: "http://browser-agent:8083",
					ContextID: "ctx",
					TaskID:    tc.task.ID,
					Task:      tc.task,
					Success:   false,
					Message:   "Task " + tc.task.ID + " is failed",
				},
			}

			out := tool.FormatForLLM(result)
			if !strings.Contains(out, "Failure reason:") {
				t.Errorf("expected output to contain 'Failure reason:', got: %s", out)
			}
			if !strings.Contains(out, errorText) {
				t.Errorf("expected output to contain underlying error text, got: %s", out)
			}
			if !strings.Contains(out, "No artifacts available") {
				t.Errorf("expected output to still mention no artifacts available, got: %s", out)
			}
		})
	}
}

func stringPtr(s string) *string { return &s }

func TestA2AQueryTaskTool_FormatPreview(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryTask: config.QueryTaskToolConfig{
					Enabled: true,
				},
			},
		},
	}
	tool := NewA2AQueryTaskTool(cfg, nil)

	tests := []struct {
		name   string
		result *domain.ToolExecutionResult
		want   string
	}{
		{
			name: "successful result with data",
			result: &domain.ToolExecutionResult{
				Data: A2AQueryTaskResult{
					Message: "Task completed successfully",
				},
			},
			want: "A2A Query Task: Task completed successfully",
		},
		{
			name: "failed result with error",
			result: &domain.ToolExecutionResult{
				Error: "Failed to connect",
			},
			want: "Failed to connect",
		},
		{
			name:   "nil result",
			result: nil,
			want:   "Tool execution result unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.FormatPreview(tt.result)
			if got != tt.want {
				t.Errorf("FormatPreview() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestA2AQueryTaskTool_PollingStateBlocking: the guard consults the supervisor's
// per-id liveness keyed by task id - not agent URL - and blocks a manual query
// only while that task's own job is still running.
func TestA2AQueryTaskTool_PollingStateBlocking(t *testing.T) {
	tests := []struct {
		name          string
		runningTaskID string
		expectBlocked bool
	}{
		{
			name:          "blocks query while the task is being polled",
			runningTaskID: "task456",
			expectBlocked: true,
		},
		{
			name:          "allows query when only a different task is running",
			runningTaskID: "other-task",
			expectBlocked: false,
		},
		{
			name:          "allows query when nothing is running",
			runningTaskID: "",
			expectBlocked: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Tools: config.A2AToolsConfig{
						QueryTask: config.QueryTaskToolConfig{
							Enabled: true,
						},
					},
				},
			}

			liveness := &mocks.FakeJobLivenessReporter{}
			liveness.IsJobRunningCalls(func(id string) bool {
				return tt.runningTaskID != "" && id == tt.runningTaskID
			})

			tool := NewA2AQueryTaskTool(cfg, liveness)

			args := map[string]any{
				"agent_url":  "http://test-agent.example.com",
				"context_id": "ctx123",
				"task_id":    "task456",
			}

			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatalf("Execute() returned unexpected error: %v", err)
			}

			if got := liveness.IsJobRunningCallCount(); got != 1 {
				t.Fatalf("IsJobRunning called %d times, want 1", got)
			}
			if got := liveness.IsJobRunningArgsForCall(0); got != "task456" {
				t.Errorf("guard keyed IsJobRunning by %q, want the task id %q", got, "task456")
			}

			if result.Success {
				t.Error("Expected Execute() to fail (blocked, or no live server)")
			}
			blocked := strings.Contains(result.Error, "background polling is active")
			if blocked != tt.expectBlocked {
				t.Errorf("blocked = %v, want %v (error: %s)", blocked, tt.expectBlocked, result.Error)
			}
		})
	}
}
