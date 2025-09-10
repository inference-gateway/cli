package tools

import (
	"context"
	"testing"
	"time"

	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestA2AQueryTool_Definition(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
		},
	}
	tool := NewA2AQueryTool(cfg)

	def := tool.Definition()

	assert.Equal(t, "Query", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "Agent-to-Agent")
}

func TestA2AQueryTool_Execute_DisabledA2A(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: false,
		},
	}
	tool := NewA2AQueryTool(cfg)

	args := map[string]any{
		"agent_url": "http://test-agent.example.com",
	}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "A2A direct connections are disabled")
}

// TODO: Update test for simplified A2A implementation
// func TestA2AQueryTool_Execute_NoService(t *testing.T) {
// 	cfg := &config.Config{
// 		A2A: config.A2AConfig{
// 			Enabled: true,
// 		},
// 	}
// 	tool := NewA2AQueryTool(cfg)
//
// 	args := map[string]any{
// 		"agent_url": "http://test-agent.example.com",
// 	}
//
// 	result, err := tool.Execute(context.Background(), args)
//
// 	require.NoError(t, err)
// 	assert.True(t, result.Success) // Now succeeds with placeholder implementation
// }

func TestA2AQueryTool_Execute_MissingAgentURL(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
		},
	}
	tool := NewA2AQueryTool(cfg)

	args := map[string]any{}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "agent_url parameter is required")
}

// TODO: Implement proper tests for simplified A2A query tool
// func TestA2AQueryTool_Execute_SuccessfulQuery(t *testing.T) {
// 	cfg := &config.Config{
// 		A2A: config.A2AConfig{
// 			Enabled: true,
// 		},
// 	}
//
// 	tool := NewA2AQueryTool(cfg)
//
// 	args := map[string]any{
// 		"agent_url": "http://test-agent.example.com",
// 	}
//
// 	result, err := tool.Execute(context.Background(), args)
//
// 	require.NoError(t, err)
// 	assert.True(t, result.Success)
// 	assert.Equal(t, "Query", result.ToolName)
// }

// TODO: Implement proper tests for simplified A2A query tool
// func TestA2AQueryTool_Execute_ServiceError(t *testing.T) {
// 	cfg := &config.Config{
// 		A2A: config.A2AConfig{
// 			Enabled: true,
// 		},
// 	}
//
// 	tool := NewA2AQueryTool(cfg)
//
// 	args := map[string]any{
// 		"agent_url": "http://test-agent.example.com",
// 	}
// }

func TestA2AQueryTool_Validate(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryTool(cfg)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid args",
			args: map[string]any{
				"agent_url": "http://test-agent.example.com",
			},
			wantErr: false,
		},
		{
			name:    "missing agent_url",
			args:    map[string]any{},
			wantErr: true,
			errMsg:  "agent_url parameter is required",
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

func TestA2AQueryTool_IsEnabled(t *testing.T) {
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
			tool := NewA2AQueryTool(cfg)

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestA2AQueryTool_FormatResult(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryTool(cfg)

	queryResult := A2AQueryResult{
		AgentName: "test-agent",
		Query:     "card",
		Response:  &adk.AgentCard{Name: "test-agent", Description: "Test agent"},
		Success:   true,
		Message:   "Query sent successfully",
		Duration:  time.Second,
	}

	result := &domain.ToolExecutionResult{
		ToolName: "Query",
		Success:  true,
		Data:     queryResult,
	}

	tests := []struct {
		name       string
		formatType domain.FormatterType
		contains   []string
	}{
		{
			name:       "LLM format",
			formatType: domain.FormatterLLM,
			contains:   []string{"A2A Query to test-agent", "Query sent successfully", "Name:test-agent"},
		},
		{
			name:       "UI format",
			formatType: domain.FormatterUI,
			contains:   []string{"**A2A Query**", "test-agent", "card", "Name:test-agent"},
		},
		{
			name:       "Short format",
			formatType: domain.FormatterShort,
			contains:   []string{"Query sent successfully"},
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

func TestA2AQueryTool_FormatPreview(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryTool(cfg)

	queryResult := A2AQueryResult{
		Success: true,
		Message: "Query sent successfully",
	}

	result := &domain.ToolExecutionResult{
		ToolName: "Query",
		Success:  true,
		Data:     queryResult,
	}

	preview := tool.FormatPreview(result)
	assert.Contains(t, preview, "A2A Query")
	assert.Contains(t, preview, "Query sent successfully")
}

func TestA2AQueryTool_ShouldCollapseArg(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryTool(cfg)

	assert.False(t, tool.ShouldCollapseArg("agent_url"))
}

func TestA2AQueryTool_ShouldAlwaysExpand(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryTool(cfg)

	assert.False(t, tool.ShouldAlwaysExpand())
}
