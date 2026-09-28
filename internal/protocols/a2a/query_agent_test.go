package a2a

import (
	"context"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	adk "github.com/inference-gateway/adk/types"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestQueryAgentTool_Definition(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryAgent: config.QueryAgentToolConfig{
					Enabled: true,
				},
			},
		},
		Prompts: *config.DefaultPromptsConfig(),
	}
	tool := NewQueryAgentTool(cfg)

	def := tool.Definition()

	assert.Equal(t, "A2A_QueryAgent", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "A2A agent")
	assert.Contains(t, *def.Function.Description, "metadata card")
}

func TestQueryAgentTool_Execute_MissingAgentURL(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				QueryAgent: config.QueryAgentToolConfig{
					Enabled: true,
				},
			},
		},
	}
	tool := NewQueryAgentTool(cfg)

	args := map[string]any{}

	result, err := tool.Execute(context.Background(), args)

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "agent_url parameter is required")
}

func TestQueryAgentTool_Validate(t *testing.T) {
	cfg := &config.Config{}
	tool := NewQueryAgentTool(cfg)

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

func TestQueryAgentTool_IsEnabled(t *testing.T) {
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
						QueryAgent: config.QueryAgentToolConfig{
							Enabled: true,
						},
					},
				},
			}
			tool := NewQueryAgentTool(cfg)

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestQueryAgentTool_FormatResult(t *testing.T) {
	cfg := &config.Config{}
	tool := NewQueryAgentTool(cfg)

	queryResult := QueryAgentResult{
		AgentName: "test-agent",
		Query:     "card",
		Response:  &adk.AgentCard{Name: "test-agent", Description: "Test agent"},
		Success:   true,
		Message:   "QueryAgent sent successfully",
		Duration:  time.Second,
	}

	result := &agentdomain.ToolExecutionResult{
		ToolName: "A2A_QueryAgent",
		Success:  true,
		Data:     queryResult,
	}

	tests := []struct {
		name       string
		formatType agentdomain.FormatterType
		contains   []string
	}{
		{
			name:       "LLM format",
			formatType: agentdomain.FormatterLLM,
			contains:   []string{"QueryAgent()", "✓ Success", "Result:", "agent_name", "test-agent", "query", "card"},
		},
		{
			name:       "UI format",
			formatType: agentdomain.FormatterUI,
			contains:   []string{"QueryAgent()", "✓ A2A QueryAgent", "QueryAgent sent successfully"},
		},
		{
			name:       "Short format",
			formatType: agentdomain.FormatterShort,
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

func TestQueryAgentTool_FormatPreview(t *testing.T) {
	cfg := &config.Config{}
	tool := NewQueryAgentTool(cfg)

	queryResult := QueryAgentResult{
		Success: true,
		Message: "QueryAgent sent successfully",
	}

	result := &agentdomain.ToolExecutionResult{
		ToolName: "A2A_QueryAgent",
		Success:  true,
		Data:     queryResult,
	}

	preview := tool.FormatPreview(result)
	assert.Contains(t, preview, "A2A QueryAgent")
	assert.Contains(t, preview, "QueryAgent sent successfully")
}

func TestQueryAgentTool_ShouldCollapseArg(t *testing.T) {
	cfg := &config.Config{}
	tool := NewQueryAgentTool(cfg)

	assert.False(t, tool.ShouldCollapseArg("agent_url"))
}

func TestQueryAgentTool_ShouldAlwaysExpand(t *testing.T) {
	cfg := &config.Config{}
	tool := NewQueryAgentTool(cfg)

	assert.False(t, tool.ShouldAlwaysExpand())
}
