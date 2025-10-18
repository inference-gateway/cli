package tools

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
	utils "github.com/inference-gateway/cli/internal/utils"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestA2ASubmitTaskTool_TaskIDTracking(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools: config.A2AToolsConfig{
				SubmitTask: config.SubmitTaskToolConfig{
					Enabled: false,
				},
			},
		},
	}

	t.Run("uses tracked task ID when available", func(t *testing.T) {
		tracker := utils.NewTaskTracker()
		agentURL := "http://test.agent"
		contextID := "context-123"

		tracker.RegisterContext(agentURL, contextID)
		tracker.AddTask(contextID, "existing-task-123")

		tool := NewA2ASubmitTaskTool(cfg, tracker)

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "Continue previous task",
		}

		_, err := tool.Execute(context.Background(), args)
		require.NoError(t, err)

		assert.True(t, tracker.HasTask("existing-task-123"))
	})

	t.Run("no task ID when tracker is empty", func(t *testing.T) {
		tracker := utils.NewTaskTracker()
		agentURL := "http://test.agent"

		tool := NewA2ASubmitTaskTool(cfg, tracker)

		args := map[string]any{
			"agent_url":        agentURL,
			"task_description": "New task",
		}

		_, err := tool.Execute(context.Background(), args)
		require.NoError(t, err)

		assert.Empty(t, tracker.GetContextsForAgent(agentURL))
	})

	t.Run("handles nil tracker gracefully", func(t *testing.T) {
		tool := NewA2ASubmitTaskTool(cfg, nil)

		args := map[string]any{
			"agent_url":        "http://test.agent",
			"task_description": "Task without tracker",
		}

		result, err := tool.Execute(context.Background(), args)
		require.NoError(t, err)
		assert.False(t, result.Success)
	})
}
