package tools

import (
	"context"
	"testing"
	"time"

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
	tool := NewA2AQueryTaskTool(cfg)
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
	tool := NewA2AQueryTaskTool(cfg)

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
	tool := NewA2AQueryTaskTool(cfg)

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
	tool := NewA2AQueryTaskTool(cfg)

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
			tool := NewA2AQueryTaskTool(cfg)

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
	tool := NewA2AQueryTaskTool(cfg)

	result := &domain.ToolExecutionResult{
		ToolName:  "A2A_QueryTask",
		Arguments: map[string]any{"agent_url": "http://example.com"},
		Success:   true,
		Duration:  time.Second,
		Data: A2AQueryTaskResult{
			AgentName: "http://example.com",
			ContextID: "ctx123",
			TaskID:    "task456",
			Status:    "completed",
			Success:   true,
			Message:   "Task task456 is completed",
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
	tool := NewA2AQueryTaskTool(cfg)

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
