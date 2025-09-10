package tools

import (
	"context"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	generated "github.com/inference-gateway/cli/tests/mocks/generated"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestNewA2ATaskTool(t *testing.T) {
	tests := []struct {
		name             string
		config           *config.Config
		a2aDirectService domain.A2ADirectService
	}{
		{
			name: "creates A2A task tool with valid inputs",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			},
			a2aDirectService: &generated.FakeA2ADirectService{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewA2ATaskTool(tt.config, tt.a2aDirectService)
			require.NotNil(t, tool)
			assert.Equal(t, tt.config, tool.config)
			assert.Equal(t, tt.a2aDirectService, tool.a2aDirectService)
		})
	}
}

func TestA2ATaskTool_Definition(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "returns valid tool definition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			}
			tool := NewA2ATaskTool(cfg, &generated.FakeA2ADirectService{})

			def := tool.Definition()

			assert.Equal(t, "Task", def.Function.Name)
			assert.NotNil(t, def.Function.Description)
			assert.NotNil(t, def.Function.Parameters)
		})
	}
}

func TestA2ATaskTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		enabled bool
	}{
		{
			name: "returns true when A2A direct is enabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			},
			enabled: true,
		},
		{
			name: "returns false when A2A direct is disabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: false,
				},
			},
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewA2ATaskTool(tt.config, &generated.FakeA2ADirectService{})

			enabled := tool.IsEnabled()

			assert.Equal(t, tt.enabled, enabled)
		})
	}
}

func TestA2ATaskTool_Validate(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name:    "fails when operation missing",
			args:    map[string]any{},
			wantErr: true,
			errMsg:  "operation parameter is required",
		},
		{
			name: "fails when operation is not string",
			args: map[string]any{
				"operation": 123,
			},
			wantErr: true,
			errMsg:  "operation parameter is required and must be a string",
		},
		{
			name: "fails for submit operation without agent_name",
			args: map[string]any{
				"operation": "submit",
			},
			wantErr: true,
			errMsg:  "agent_name parameter is required for submit operation",
		},
		{
			name: "fails for submit operation without task_type",
			args: map[string]any{
				"operation":  "submit",
				"agent_name": "test-agent",
			},
			wantErr: true,
			errMsg:  "task_type parameter is required for submit operation",
		},
		{
			name: "fails for submit operation without task_description",
			args: map[string]any{
				"operation":  "submit",
				"agent_name": "test-agent",
				"task_type":  "test",
			},
			wantErr: true,
			errMsg:  "task_description parameter is required for submit operation",
		},
		{
			name: "succeeds for valid submit operation",
			args: map[string]any{
				"operation":        "submit",
				"agent_name":       "test-agent",
				"task_type":        "test",
				"task_description": "Test task",
			},
			wantErr: false,
		},
		{
			name: "fails for status operation without task_id",
			args: map[string]any{
				"operation": "status",
			},
			wantErr: true,
			errMsg:  "task_id parameter is required for status operation",
		},
		{
			name: "succeeds for valid status operation",
			args: map[string]any{
				"operation": "status",
				"task_id":   "test-task",
			},
			wantErr: false,
		},
		{
			name: "succeeds for list_agents operation",
			args: map[string]any{
				"operation": "list_agents",
			},
			wantErr: false,
		},
		{
			name: "fails for unknown operation",
			args: map[string]any{
				"operation": "unknown",
			},
			wantErr: true,
			errMsg:  "unknown operation: unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			}
			tool := NewA2ATaskTool(cfg, &generated.FakeA2ADirectService{})

			err := tool.Validate(tt.args)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestA2ATaskTool_Execute_Submit(t *testing.T) {
	tests := []struct {
		name             string
		config           *config.Config
		args             map[string]any
		mockSetup        func(*generated.FakeA2ADirectService)
		expectSuccess    bool
		expectedTaskID   string
		expectedErrorMsg string
	}{
		{
			name: "fails when A2A direct disabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: false,
				},
			},
			args: map[string]any{
				"operation":        "submit",
				"agent_name":       "test-agent",
				"task_type":        "test",
				"task_description": "Test task",
			},
			mockSetup:        func(m *generated.FakeA2ADirectService) {},
			expectSuccess:    false,
			expectedErrorMsg: "A2A direct connections are disabled",
		},
		{
			name: "submits task successfully",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			},
			args: map[string]any{
				"operation":        "submit",
				"agent_name":       "test-agent",
				"task_type":        "test",
				"task_description": "Test task",
			},
			mockSetup: func(m *generated.FakeA2ADirectService) {
				m.SubmitTaskReturns("task-123", nil)
			},
			expectSuccess:  true,
			expectedTaskID: "task-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := &generated.FakeA2ADirectService{}
			tt.mockSetup(mockService)

			tool := NewA2ATaskTool(tt.config, mockService)
			ctx := context.Background()

			result, err := tool.Execute(ctx, tt.args)

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, "Task", result.ToolName)
			assert.Equal(t, tt.expectSuccess, result.Success)

			if tt.expectSuccess {
				data, ok := result.Data.(A2ATaskResult)
				require.True(t, ok)
				assert.Equal(t, "submit", data.Operation)
				assert.Equal(t, tt.expectedTaskID, data.TaskID)
				assert.True(t, data.Success)
			} else {
				assert.Contains(t, result.Error, tt.expectedErrorMsg)
			}

		})
	}
}

func TestA2ATaskTool_Execute_Status(t *testing.T) {
	tests := []struct {
		name             string
		args             map[string]any
		mockSetup        func(*generated.FakeA2ADirectService)
		expectSuccess    bool
		expectedStatus   domain.A2ATaskStatusEnum
		expectedErrorMsg string
	}{
		{
			name: "gets task status successfully",
			args: map[string]any{
				"operation": "status",
				"task_id":   "test-task",
			},
			mockSetup: func(m *generated.FakeA2ADirectService) {
				status := &domain.A2ATaskStatus{
					TaskID:    "test-task",
					Status:    domain.A2ATaskStatusWorking,
					Progress:  75.0,
					Message:   "Task in progress",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				m.GetTaskStatusReturns(status, nil)
			},
			expectSuccess:  true,
			expectedStatus: domain.A2ATaskStatusWorking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := &generated.FakeA2ADirectService{}
			tt.mockSetup(mockService)

			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			}
			tool := NewA2ATaskTool(cfg, mockService)
			ctx := context.Background()

			result, err := tool.Execute(ctx, tt.args)

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectSuccess, result.Success)

			if tt.expectSuccess {
				data, ok := result.Data.(A2ATaskResult)
				require.True(t, ok)
				assert.Equal(t, "status", data.Operation)
				assert.Equal(t, tt.expectedStatus, data.Status)
			}

		})
	}
}

func TestA2ATaskTool_Execute_ListAgents(t *testing.T) {
	tests := []struct {
		name          string
		mockSetup     func(*generated.FakeA2ADirectService)
		expectSuccess bool
		expectedCount int
	}{
		{
			name: "lists agents successfully",
			mockSetup: func(m *generated.FakeA2ADirectService) {
				agents := map[string]domain.A2AAgentInfo{
					"agent1": {
						Name:        "agent1",
						URL:         "http://localhost:8081",
						Description: "Test agent 1",
						Enabled:     true,
					},
					"agent2": {
						Name:        "agent2",
						URL:         "http://localhost:8082",
						Description: "Test agent 2",
						Enabled:     true,
					},
				}
				m.ListActiveAgentsReturns(agents, nil)
			},
			expectSuccess: true,
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := &generated.FakeA2ADirectService{}
			tt.mockSetup(mockService)

			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			}
			tool := NewA2ATaskTool(cfg, mockService)
			ctx := context.Background()

			args := map[string]any{
				"operation": "list_agents",
			}

			result, err := tool.Execute(ctx, args)

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectSuccess, result.Success)

			if tt.expectSuccess {
				data, ok := result.Data.(A2ATaskResult)
				require.True(t, ok)
				assert.Equal(t, "list_agents", data.Operation)

				if agentList, ok := data.Result.([]map[string]any); ok {
					assert.Len(t, agentList, tt.expectedCount)
				}
			}

		})
	}
}

func TestA2ATaskTool_FormatResult(t *testing.T) {
	tests := []struct {
		name       string
		result     *domain.ToolExecutionResult
		formatType domain.FormatterType
		expected   string
	}{
		{
			name: "formats for UI",
			result: &domain.ToolExecutionResult{
				Data: A2ATaskResult{
					Operation: "submit",
					TaskID:    "test-task",
					AgentName: "test-agent",
					Status:    "submitted",
					Success:   true,
					Message:   "Task submitted successfully",
				},
			},
			formatType: domain.FormatterUI,
			expected:   "**A2A submit**: Task submitted successfully",
		},
		{
			name: "formats for LLM",
			result: &domain.ToolExecutionResult{
				Data: A2ATaskResult{
					Operation: "status",
					TaskID:    "test-task",
					Success:   true,
					Message:   "Task is running",
				},
			},
			formatType: domain.FormatterLLM,
			expected:   "A2A Task status: Task is running (Task ID: test-task)",
		},
		{
			name: "formats short",
			result: &domain.ToolExecutionResult{
				Data: A2ATaskResult{
					Operation: "collect",
					Success:   true,
					Message:   "Results collected",
				},
			},
			formatType: domain.FormatterShort,
			expected:   "Results collected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			}
			tool := NewA2ATaskTool(cfg, &generated.FakeA2ADirectService{})

			formatted := tool.FormatResult(tt.result, tt.formatType)

			assert.Contains(t, formatted, tt.expected)
		})
	}
}

func TestA2ATaskTool_ShouldCollapseArg(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{
			name:     "collapses parameters",
			key:      "parameters",
			expected: true,
		},
		{
			name:     "does not collapse other args",
			key:      "operation",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			}
			tool := NewA2ATaskTool(cfg, &generated.FakeA2ADirectService{})

			result := tool.ShouldCollapseArg(tt.key)

			assert.Equal(t, tt.expected, result)
		})
	}
}
