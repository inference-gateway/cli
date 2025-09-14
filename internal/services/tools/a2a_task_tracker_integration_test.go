package tools

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
	utils "github.com/inference-gateway/cli/internal/utils"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestA2ATaskTool_TaskIDTracking(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Task: config.TaskToolConfig{
				Enabled: false,
			},
		},
	}

	t.Run("uses tracked task ID when available", func(t *testing.T) {
		tracker := utils.NewSimpleTaskTracker()
		tracker.SetFirstTaskID("existing-task-123")

		tool := NewA2ATaskTool(cfg, tracker)

		args := map[string]any{
			"agent_url":        "http://test.agent",
			"task_description": "Continue previous task",
		}

		_, err := tool.Execute(context.Background(), args)
		require.NoError(t, err)

		assert.Equal(t, "existing-task-123", tracker.GetFirstTaskID())
	})

	t.Run("no task ID when tracker is empty", func(t *testing.T) {
		tracker := utils.NewSimpleTaskTracker()

		tool := NewA2ATaskTool(cfg, tracker)

		args := map[string]any{
			"agent_url":        "http://test.agent",
			"task_description": "New task",
		}

		_, err := tool.Execute(context.Background(), args)
		require.NoError(t, err)

		assert.Equal(t, "", tracker.GetFirstTaskID())
	})

	t.Run("handles nil tracker gracefully", func(t *testing.T) {
		tool := NewA2ATaskTool(cfg, nil)

		args := map[string]any{
			"agent_url":        "http://test.agent",
			"task_description": "Task without tracker",
		}

		result, err := tool.Execute(context.Background(), args)
		require.NoError(t, err)
		assert.False(t, result.Success)
	})
}
