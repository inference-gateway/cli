package a2a

import (
	config "github.com/inference-gateway/cli/config"
	a2adomain "github.com/inference-gateway/cli/internal/a2a/domain"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// NewTools builds the A2A tool set, empty unless a2a.enabled. Submitted tasks
// share the session's tracker, run as jobs of the supervisor behind jobs, and
// leave their outcome in retention once they finish.
func NewTools(cfg *config.Config, tracker a2adomain.TaskTracker, jobs scheddomain.BackgroundTaskRegistry, retention a2adomain.TaskRetentionService) map[string]agentdomain.Tool {
	if !cfg.IsA2AToolsEnabled() {
		return nil
	}
	return map[string]agentdomain.Tool{
		ToolQueryAgent: NewQueryAgentTool(cfg),
		ToolQueryTask:  NewQueryTaskTool(cfg, jobs),
		ToolSubmitTask: NewSubmitTaskTool(cfg, tracker, jobs, retention),
	}
}
