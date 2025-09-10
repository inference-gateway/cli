package tools

import (
	"context"
	"testing"
	"time"

	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestA2ATaskTool_Definition(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
		},
	}
	tool := NewA2ATaskTool(cfg)

	def := tool.Definition()

	assert.Equal(t, "Task", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "Agent-to-Agent")
}

func TestA2ATaskTool_Execute_DisabledA2A(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: false,
		},
	}
	tool := NewA2ATaskTool(cfg)

	args := map[string]any{
		"agent_url":        "http://test-agent.example.com",
		"task_description": "Test task",
	}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "A2A direct connections are disabled")
}

// TODO: Update test for simplified A2A implementation
// func TestA2ATaskTool_Execute_NoService(t *testing.T) {
// 	cfg := &config.Config{
// 		A2A: config.A2AConfig{
// 			Enabled: true,
// 		},
// 	}
// 	tool := NewA2ATaskTool(cfg)
//
// 	args := map[string]any{
// 		"agent_url":        "http://test-agent.example.com",
// 		"task_description": "Test task",
// 	}
//
// 	result, err := tool.Execute(context.Background(), args)
//
// 	require.NoError(t, err)
// 	assert.True(t, result.Success) // Now succeeds with placeholder implementation
// }

func TestA2ATaskTool_Execute_MissingAgentURL(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
		},
	}
	tool := NewA2ATaskTool(cfg)

	args := map[string]any{
		"task_description": "Test task",
	}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "agent_url parameter is required")
}

func TestA2ATaskTool_Execute_MissingTaskDescription(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
		},
	}
	tool := NewA2ATaskTool(cfg)

	args := map[string]any{
		"agent_url": "http://test-agent.example.com",
	}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "task_description parameter is required")
}

// TODO: Implement proper tests for simplified A2A task tool
// func TestA2ATaskTool_Execute_SuccessfulSubmit(t *testing.T) {
// 	cfg := &config.Config{
// 		A2A: config.A2AConfig{
// 			Enabled: true,
// 		},
// 	}
//
// 	tool := NewA2ATaskTool(cfg)
//
// 	args := map[string]any{
// 		"agent_url":        "http://test-agent.example.com",
// 		"task_description": "Test task",
// 	}
//
// 	result, err := tool.Execute(context.Background(), args)
//
// 	require.NoError(t, err)
// 	assert.True(t, result.Success)
// 	assert.Equal(t, "Task", result.ToolName)
// }

func TestA2ATaskTool_Validate(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ATaskTool(cfg)

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

func TestA2ATaskTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enabled when A2A is enabled",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disabled when A2A is disabled",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: tt.enabled,
				},
			}
			tool := NewA2ATaskTool(cfg)

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestA2ATaskTool_FormatResult(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ATaskTool(cfg)

	taskResult := A2ATaskResult{
		AgentName: "test-agent",
		Task: &adk.Task{
			ID:   "task-123",
			Kind: "test",
		},
		Success:  true,
		Message:  "Task submitted successfully",
		Duration: time.Second,
	}

	result := &domain.ToolExecutionResult{
		ToolName: "Task",
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
			contains:   []string{"A2A Task", "Task submitted successfully", "task-123"},
		},
		{
			name:       "UI format",
			formatType: domain.FormatterUI,
			contains:   []string{"**A2A Task**", "test-agent", "task-123", "test"},
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

func TestA2ATaskTool_FormatPreview(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ATaskTool(cfg)

	taskResult := A2ATaskResult{
		Success: true,
		Message: "Task submitted successfully",
	}

	result := &domain.ToolExecutionResult{
		ToolName: "Task",
		Success:  true,
		Data:     taskResult,
	}

	preview := tool.FormatPreview(result)
	assert.Contains(t, preview, "A2A Task")
	assert.Contains(t, preview, "Task submitted successfully")
}

func TestA2ATaskTool_ShouldCollapseArg(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ATaskTool(cfg)

	assert.True(t, tool.ShouldCollapseArg("metadata"))
	assert.False(t, tool.ShouldCollapseArg("agent_url"))
	assert.False(t, tool.ShouldCollapseArg("task_description"))
}

func TestA2ATaskTool_ShouldAlwaysExpand(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2ATaskTool(cfg)

	assert.False(t, tool.ShouldAlwaysExpand())
}
