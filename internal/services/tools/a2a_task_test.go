package tools

import (
	"context"
	"testing"

	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
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
	}
	tool := NewA2ASubmitTaskTool(cfg, nil)

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
	tool := NewA2ASubmitTaskTool(cfg, nil)

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
	tool := NewA2ASubmitTaskTool(cfg, nil)

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
	tool := NewA2ASubmitTaskTool(cfg, nil)

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
			tool := NewA2ASubmitTaskTool(cfg, nil)

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestA2ASubmitTaskTool_FormatResult(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil)

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
			contains:   []string{"Task()", "✓ Success", "Result:", "task_id", "task-123"},
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

func TestA2ASubmitTaskTool_FormatPreview(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil)

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
	tool := NewA2ASubmitTaskTool(cfg, nil)

	assert.True(t, tool.ShouldCollapseArg("metadata"))
	assert.False(t, tool.ShouldCollapseArg("agent_url"))
	assert.False(t, tool.ShouldCollapseArg("task_description"))
}

func TestA2ASubmitTaskTool_ShouldAlwaysExpand(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ASubmitTaskTool(cfg, nil)

	assert.False(t, tool.ShouldAlwaysExpand())
}
