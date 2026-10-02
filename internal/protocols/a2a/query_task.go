package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	adk "github.com/inference-gateway/adk/types"
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	a2ainfra "github.com/inference-gateway/cli/internal/protocols/a2a/infrastructure"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	tools "github.com/inference-gateway/cli/internal/tools"
)

type QueryTaskTool struct {
	config    *config.Config
	formatter tools.CustomFormatter
	liveness  scheddomain.JobLivenessReporter
}

type QueryTaskResult struct {
	AgentName string        `json:"agent_name"`
	ContextID string        `json:"context_id"`
	TaskID    string        `json:"task_id"`
	Task      *adk.Task     `json:"task"`
	Success   bool          `json:"success"`
	Message   string        `json:"message"`
	Duration  time.Duration `json:"duration"`
}

func NewQueryTaskTool(cfg *config.Config, liveness scheddomain.JobLivenessReporter) *QueryTaskTool {
	return &QueryTaskTool{
		config: cfg,
		formatter: tools.NewCustomFormatter(ToolQueryTask, func(key string) bool {
			return key == "metadata"
		}),
		liveness: liveness,
	}
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *QueryTaskTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolQueryTask).WithRequireApproval(t.config.A2A.Tools.QueryTask.RequireApproval)
}

func (t *QueryTaskTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

func (t *QueryTaskTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.IsEnabled() {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolQueryTask,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: QueryTaskResult{
				Success: false,
				Message: "A2A connections are disabled",
			},
		}, nil
	}

	agentURL, ok := args["agent_url"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_url parameter is required and must be a string")
	}

	contextID, ok := args["context_id"].(string)
	if !ok {
		return t.errorResult(args, startTime, "context_id parameter is required and must be a string")
	}

	taskID, ok := args["task_id"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_id parameter is required and must be a string")
	}

	if t.liveness != nil && t.liveness.IsJobRunning(taskID) {
		return t.errorResult(args, startTime, t.buildPollingBlockedError(agentURL))
	}

	adkClient := a2ainfra.NewClient(agentURL)
	queryParams := adk.GetTaskRequest{ID: taskID}
	taskResponse, err := adkClient.GetTask(ctx, queryParams)
	if err != nil {
		logger.Error("failed to query task", "agent_url", agentURL, "task_id", taskID, "error", err)
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to query task: %v", err))
	}

	taskBytes, err := json.Marshal(taskResponse.Result)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to marshal task result: %v", err))
	}

	var task adk.Task
	if err := json.Unmarshal(taskBytes, &task); err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to unmarshal task: %v", err))
	}

	result := QueryTaskResult{
		AgentName: agentURL,
		ContextID: contextID,
		TaskID:    taskID,
		Task:      &task,
		Success:   true,
		Duration:  time.Since(startTime),
	}

	result.Message = fmt.Sprintf("Task %s is %s", taskID, task.Status.State)

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolQueryTask,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data:      result,
	}, nil
}

// buildPollingBlockedError explains that the supervisor is already polling this
// task, so a manual query should defer to it. The message is intentionally
// generic: liveness now comes from the supervisor, which does not carry the
// next-poll timing the old A2ATracker state did.
func (t *QueryTaskTool) buildPollingBlockedError(agentURL string) string {
	return fmt.Sprintf("Cannot query task manually - background polling is active for agent %s. The A2A_SubmitTask tool is already polling for updates automatically. Please wait for the polling to complete.", agentURL)
}

func (t *QueryTaskTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*agentdomain.ToolExecutionResult, error) {
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolQueryTask,
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: QueryTaskResult{
			Success: false,
			Message: errorMsg,
		},
	}, nil
}

func (t *QueryTaskTool) Validate(args map[string]any) error {
	if _, ok := args["agent_url"].(string); !ok {
		return fmt.Errorf("agent_url parameter is required and must be a string")
	}
	if _, ok := args["context_id"].(string); !ok {
		return fmt.Errorf("context_id parameter is required and must be a string")
	}
	if _, ok := args["task_id"].(string); !ok {
		return fmt.Errorf("task_id parameter is required and must be a string")
	}
	return nil
}

func (t *QueryTaskTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterUI:
		return t.FormatForUI(result)
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

func (t *QueryTaskTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Data == nil {
		return t.formatter.FormatExpanded(result, "")
	}

	queryResult, ok := result.Data.(QueryTaskResult)
	if !ok || queryResult.Task == nil {
		return t.formatter.FormatExpanded(result, t.formatter.FormatAsJSON(result.Data))
	}

	var body strings.Builder
	task := queryResult.Task
	fmt.Fprintf(&body, "Task Status: %s\n", task.Status.State)

	if a2adomain.NormalizeTaskState(task.Status.State) == adk.TaskStateFailed {
		if reason := failureReasonFromTask(*task); reason != "" {
			fmt.Fprintf(&body, "\nFailure reason: %s\n", reason)
		}
	}

	hasArtifacts := len(task.Artifacts) > 0
	if !hasArtifacts {
		body.WriteString("\nNo artifacts available for this task.\n")
		body.WriteString("\nFull Task Data:\n")
		body.WriteString(t.formatter.FormatAsJSON(result.Data))
		return t.formatter.FormatExpanded(result, body.String())
	}

	fmt.Fprintf(&body, "\nArtifacts (%d):\n", len(task.Artifacts))
	for i, artifact := range task.Artifacts {
		fmt.Fprintf(&body, "%d. ", i+1)
		if artifact.Name != nil {
			fmt.Fprintf(&body, "Name: %s", *artifact.Name)
		}
		fmt.Fprintf(&body, " (ID: %s)", artifact.ArtifactID)

		if artifact.Metadata != nil {
			if url, ok := (*artifact.Metadata)["url"].(string); ok {
				fmt.Fprintf(&body, "\n   Download URL: %s", url)
			}
			if mimeType, ok := (*artifact.Metadata)["mime_type"].(string); ok {
				fmt.Fprintf(&body, "\n   MIME Type: %s", mimeType)
			}
			if size, ok := (*artifact.Metadata)["size"].(float64); ok {
				fmt.Fprintf(&body, "\n   Size: %d bytes", int64(size))
			}
		}
		body.WriteString("\n")
	}

	body.WriteString("\nFull Task Data:\n")
	body.WriteString(t.formatter.FormatAsJSON(result.Data))
	return t.formatter.FormatExpanded(result, body.String())
}

func (t *QueryTaskTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", toolCall)
	fmt.Fprintf(&output, "└─ %s %s", statusIcon, preview)

	return output.String()
}

func (t *QueryTaskTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(QueryTaskResult); ok {
		return fmt.Sprintf("A2A Query Task: %s", data.Message)
	}

	return "A2A query task operation completed"
}

func (t *QueryTaskTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

func (t *QueryTaskTool) ShouldAlwaysExpand() bool {
	return false
}

func (t *QueryTaskTool) IsEnabled() bool {
	return t.config.IsA2AToolsEnabled() && t.config.A2A.Tools.QueryTask.Enabled
}

// SelfGated implements agentdomain.SelfGatedTool: a2a.enabled, not
// tools.enabled, switches the A2A tools on.
func (t *QueryTaskTool) SelfGated() bool { return true }
