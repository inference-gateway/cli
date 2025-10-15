package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
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

type mockTaskTracker struct {
	isPolling bool
}

func (m *mockTaskTracker) GetTaskIDForAgent(agentURL string) string                     { return "" }
func (m *mockTaskTracker) SetTaskIDForAgent(agentURL, taskID string)                    {}
func (m *mockTaskTracker) ClearTaskIDForAgent(agentURL string)                          {}
func (m *mockTaskTracker) GetContextIDForAgent(agentURL string) string                  { return "" }
func (m *mockTaskTracker) SetContextIDForAgent(agentURL, contextID string)              {}
func (m *mockTaskTracker) ClearAllAgents()                                              {}
func (m *mockTaskTracker) StartPolling(agentURL string, state *domain.TaskPollingState) {}
func (m *mockTaskTracker) StopPolling(agentURL string)                                  {}
func (m *mockTaskTracker) GetPollingState(agentURL string) *domain.TaskPollingState     { return nil }
func (m *mockTaskTracker) IsPolling(agentURL string) bool                               { return m.isPolling }
func (m *mockTaskTracker) GetAllPollingAgents() []string                                { return []string{} }

func TestA2AQueryTaskTool_PollingStateBlocking(t *testing.T) {
	tests := []struct {
		name                 string
		isPolling            bool
		expectBlocked        bool
		expectedErrorMessage string
	}{
		{
			name:                 "blocks query when polling is active",
			isPolling:            true,
			expectBlocked:        true,
			expectedErrorMessage: "Cannot query task manually - background polling is active",
		},
		{
			name:          "allows query when polling is not active",
			isPolling:     false,
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

			tracker := &mockTaskTracker{
				isPolling: tt.isPolling,
			}

			tool := NewA2AQueryTaskTool(cfg, tracker)

			args := map[string]any{
				"agent_url":  "http://test-agent.example.com",
				"context_id": "ctx123",
				"task_id":    "task456",
			}

			result, err := tool.Execute(context.Background(), args)

			if err != nil {
				t.Errorf("Execute() returned unexpected error: %v", err)
			}

			if !tt.expectBlocked {
				if result.Success {
					t.Error("Expected Execute() to fail (no server available for test)")
				}
				return
			}

			if result.Success {
				t.Error("Expected Execute() to fail when polling is active")
			}

			if !strings.Contains(result.Error, tt.expectedErrorMessage) {
				t.Errorf("Expected error to contain %q, got: %s", tt.expectedErrorMessage, result.Error)
			}
		})
	}
}
