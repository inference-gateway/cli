package tools

import (
	"context"
	"os"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestA2ADownloadArtifactsTool_Execute(t *testing.T) {
	tests := []struct {
		name         string
		config       *config.Config
		args         map[string]any
		wantSuccess  bool
		wantErrorMsg string
	}{
		{
			name: "missing agent_url",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							Enabled: true,
						},
					},
				},
			},
			args: map[string]any{
				"context_id": "context-123",
				"task_id":    "task-456",
			},
			wantSuccess:  false,
			wantErrorMsg: "agent_url parameter is required and must be a string",
		},
		{
			name: "missing context_id",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							Enabled: true,
						},
					},
				},
			},
			args: map[string]any{
				"agent_url": "http://localhost:8081",
				"task_id":   "task-456",
			},
			wantSuccess:  false,
			wantErrorMsg: "context_id parameter is required and must be a string",
		},
		{
			name: "missing task_id",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							Enabled: true,
						},
					},
				},
			},
			args: map[string]any{
				"agent_url":  "http://localhost:8081",
				"context_id": "context-123",
			},
			wantSuccess:  false,
			wantErrorMsg: "task_id parameter is required and must be a string",
		},
		{
			name: "A2A disabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: false,
				},
			},
			args: map[string]any{
				"agent_url":  "http://localhost:8081",
				"context_id": "context-123",
				"task_id":    "task-456",
			},
			wantSuccess:  false,
			wantErrorMsg: "A2A connections are disabled in configuration",
		},
		{
			name: "tool disabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							Enabled: false,
						},
					},
				},
			},
			args: map[string]any{
				"agent_url":  "http://localhost:8081",
				"context_id": "context-123",
				"task_id":    "task-456",
			},
			wantSuccess:  false,
			wantErrorMsg: "A2A connections are disabled in configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := utils.NewSimpleTaskTracker()
			tool := NewA2ADownloadArtifactsTool(tt.config, tracker)

			result, err := tool.Execute(context.Background(), tt.args)

			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, tt.wantSuccess, result.Success)

			if tt.wantErrorMsg != "" {
				assert.Contains(t, result.Error, tt.wantErrorMsg)
			}
		})
	}
}

func TestA2ADownloadArtifactsTool_Validate(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name: "valid args",
			args: map[string]any{
				"agent_url":  "http://localhost:8081",
				"context_id": "context-123",
				"task_id":    "task-456",
			},
			wantErr: "",
		},
		{
			name: "missing agent_url",
			args: map[string]any{
				"context_id": "context-123",
				"task_id":    "task-456",
			},
			wantErr: "agent_url parameter is required and must be a string",
		},
		{
			name: "missing context_id",
			args: map[string]any{
				"agent_url": "http://localhost:8081",
				"task_id":   "task-456",
			},
			wantErr: "context_id parameter is required and must be a string",
		},
		{
			name: "missing task_id",
			args: map[string]any{
				"agent_url":  "http://localhost:8081",
				"context_id": "context-123",
			},
			wantErr: "task_id parameter is required and must be a string",
		},
		{
			name: "invalid agent_url type",
			args: map[string]any{
				"agent_url":  123,
				"context_id": "context-123",
				"task_id":    "task-456",
			},
			wantErr: "agent_url parameter is required and must be a string",
		},
		{
			name: "invalid context_id type",
			args: map[string]any{
				"agent_url":  "http://localhost:8081",
				"context_id": 123,
				"task_id":    "task-456",
			},
			wantErr: "context_id parameter is required and must be a string",
		},
		{
			name: "invalid task_id type",
			args: map[string]any{
				"agent_url":  "http://localhost:8081",
				"context_id": "context-123",
				"task_id":    123,
			},
			wantErr: "task_id parameter is required and must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tracker := utils.NewSimpleTaskTracker()
			tool := NewA2ADownloadArtifactsTool(cfg, tracker)

			err := tool.Validate(tt.args)

			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestA2ADownloadArtifactsTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		want   bool
	}{
		{
			name: "enabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							Enabled: true,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "A2A disabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: false,
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							Enabled: true,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "tool disabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							Enabled: false,
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := utils.NewSimpleTaskTracker()
			tool := NewA2ADownloadArtifactsTool(tt.config, tracker)

			got := tool.IsEnabled()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestA2ADownloadArtifactsTool_Definition(t *testing.T) {
	cfg := &config.Config{}
	tracker := utils.NewSimpleTaskTracker()
	tool := NewA2ADownloadArtifactsTool(cfg, tracker)

	def := tool.Definition()

	assert.Equal(t, "A2A_DownloadArtifacts", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "Download artifacts from a completed A2A task")
	assert.Contains(t, *def.Function.Description, "completed")

	params := def.Function.Parameters
	assert.NotNil(t, params)

	properties, ok := (*params)["properties"].(map[string]interface{})
	assert.True(t, ok)

	assert.Contains(t, properties, "agent_url")
	assert.Contains(t, properties, "context_id")
	assert.Contains(t, properties, "task_id")

	required, ok := (*params)["required"].([]string)
	assert.True(t, ok)
	assert.Contains(t, required, "agent_url")
	assert.Contains(t, required, "context_id")
	assert.Contains(t, required, "task_id")
}

func TestA2ADownloadArtifactsTool_FormatResult(t *testing.T) {
	tests := []struct {
		name       string
		result     *domain.ToolExecutionResult
		formatType domain.FormatterType
		wantText   string
	}{
		{
			name: "successful result - UI format",
			result: &domain.ToolExecutionResult{
				ToolName:  "A2A_DownloadArtifacts",
				Arguments: map[string]any{"agent_url": "http://localhost:8081"},
				Success:   true,
				Data: A2ADownloadArtifactsResult{
					Message: "Downloaded 2 artifact(s) from completed task task-123",
				},
			},
			formatType: domain.FormatterUI,
			wantText:   "A2A Download Artifacts: Downloaded 2 artifact(s) from completed task task-123",
		},
		{
			name: "error result - UI format",
			result: &domain.ToolExecutionResult{
				ToolName:  "A2A_DownloadArtifacts",
				Arguments: map[string]any{"agent_url": "http://localhost:8081"},
				Success:   false,
				Error:     "Task not completed",
			},
			formatType: domain.FormatterUI,
			wantText:   "Task not completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tracker := utils.NewSimpleTaskTracker()
			tool := NewA2ADownloadArtifactsTool(cfg, tracker)

			got := tool.FormatResult(tt.result, tt.formatType)
			assert.Contains(t, got, tt.wantText)
		})
	}
}

func TestA2ADownloadArtifactsTool_ShouldCollapseArg(t *testing.T) {
	cfg := &config.Config{}
	tracker := utils.NewSimpleTaskTracker()
	tool := NewA2ADownloadArtifactsTool(cfg, tracker)

	tests := []struct {
		key  string
		want bool
	}{
		{"content", true},
		{"agent_url", false},
		{"task_id", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := tool.ShouldCollapseArg(tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestA2ADownloadArtifactsTool_ShouldAlwaysExpand(t *testing.T) {
	cfg := &config.Config{}
	tracker := utils.NewSimpleTaskTracker()
	tool := NewA2ADownloadArtifactsTool(cfg, tracker)

	got := tool.ShouldAlwaysExpand()
	assert.False(t, got)
}

func TestA2ADownloadArtifactsTool_getDownloadDirectory(t *testing.T) {
	tests := []struct {
		name           string
		config         *config.Config
		expectedResult string
	}{
		{
			name: "configured download directory",
			config: &config.Config{
				A2A: config.A2AConfig{
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							DownloadDir: "/custom/download/path",
						},
					},
				},
			},
			expectedResult: "/custom/download/path",
		},
		{
			name: "default download directory",
			config: &config.Config{
				A2A: config.A2AConfig{
					Tools: config.A2AToolsConfig{
						DownloadArtifacts: config.DownloadArtifactsToolConfig{
							DownloadDir: "",
						},
					},
				},
			},
			expectedResult: "./downloads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := utils.NewSimpleTaskTracker()
			tool := NewA2ADownloadArtifactsTool(tt.config, tracker)

			result := tool.getDownloadDirectory()
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestA2ADownloadArtifactsTool_ensureDownloadDirectory(t *testing.T) {
	tracker := utils.NewSimpleTaskTracker()
	tool := NewA2ADownloadArtifactsTool(&config.Config{}, tracker)

	tempDir := t.TempDir()
	testDir := tempDir + "/test/nested/directory"

	err := tool.ensureDownloadDirectory(testDir)
	assert.NoError(t, err)

	info, err := os.Stat(testDir)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())
}
