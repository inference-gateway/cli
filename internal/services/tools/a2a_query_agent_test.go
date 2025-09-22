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

func TestA2AQueryAgentTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			QueryAgent: config.QueryAgentToolConfig{
				Enabled: true,
			},
		},
	}
	tool := NewA2AQueryAgentTool(cfg)

	def := tool.Definition()

	assert.Equal(t, "QueryAgent", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "A2A agent")
	assert.Contains(t, *def.Function.Description, "metadata card")
}

func TestA2AQueryAgentTool_Execute_MissingAgentURL(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			QueryAgent: config.QueryAgentToolConfig{
				Enabled: true,
			},
		},
	}
	tool := NewA2AQueryAgentTool(cfg)

	args := map[string]any{}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "agent_url parameter is required")
}

func TestA2AQueryAgentTool_Validate(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryAgentTool(cfg)

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

func TestA2AQueryAgentTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		a2aEnabled bool
		expected   bool
	}{
		{
			name:       "enabled when query agent tool is enabled",
			enabled:    true,
			a2aEnabled: false,
			expected:   true,
		},
		{
			name:       "disabled when both query agent tool and A2A are disabled",
			enabled:    false,
			a2aEnabled: false,
			expected:   false,
		},
		{
			name:       "enabled when A2A is enabled even if query agent tool is disabled",
			enabled:    false,
			a2aEnabled: true,
			expected:   true,
		},
		{
			name:       "enabled when both query agent tool and A2A are enabled",
			enabled:    true,
			a2aEnabled: true,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					QueryAgent: config.QueryAgentToolConfig{
						Enabled: tt.enabled,
					},
				},
				A2A: config.A2AConfig{
					Enabled: tt.a2aEnabled,
				},
			}
			tool := NewA2AQueryAgentTool(cfg)

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestA2AQueryAgentTool_FormatResult(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryAgentTool(cfg)

	queryResult := A2AQueryAgentResult{
		AgentName: "test-agent",
		Query:     "card",
		Response:  &adk.AgentCard{Name: "test-agent", Description: "Test agent"},
		Success:   true,
		Message:   "QueryAgent sent successfully",
		Duration:  time.Second,
	}

	result := &domain.ToolExecutionResult{
		ToolName: "QueryAgent",
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
			contains:   []string{"QueryAgent()", "✓ Success", "📄 Result:", "agent_name", "test-agent", "query", "card"},
		},
		{
			name:       "UI format",
			formatType: domain.FormatterUI,
			contains:   []string{"QueryAgent()", "✓ A2A QueryAgent", "QueryAgent sent successfully"},
		},
		{
			name:       "Short format",
			formatType: domain.FormatterShort,
			contains:   []string{"QueryAgent sent successfully"},
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

func TestA2AQueryAgentTool_FormatPreview(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryAgentTool(cfg)

	queryResult := A2AQueryAgentResult{
		Success: true,
		Message: "QueryAgent sent successfully",
	}

	result := &domain.ToolExecutionResult{
		ToolName: "QueryAgent",
		Success:  true,
		Data:     queryResult,
	}

	preview := tool.FormatPreview(result)
	assert.Contains(t, preview, "A2A QueryAgent")
	assert.Contains(t, preview, "QueryAgent sent successfully")
}

func TestA2AQueryAgentTool_ShouldCollapseArg(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryAgentTool(cfg)

	assert.False(t, tool.ShouldCollapseArg("agent_url"))
}

func TestA2AQueryAgentTool_ShouldAlwaysExpand(t *testing.T) {
	cfg := &config.Config{}
	tool := NewA2AQueryAgentTool(cfg)

	assert.False(t, tool.ShouldAlwaysExpand())
}
