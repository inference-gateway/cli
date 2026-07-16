package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
)

type A2AQueryTaskTool struct {
	config    *config.Config
	formatter domain.CustomFormatter
	liveness  domain.JobLivenessReporter
}

type A2AQueryTaskResult struct {
	AgentName string        `json:"agent_name"`
	ContextID string        `json:"context_id"`
	TaskID    string        `json:"task_id"`
	Task      *adk.Task     `json:"task"`
	Success   bool          `json:"success"`
	Message   string        `json:"message"`
	Duration  time.Duration `json:"duration"`
}

func NewA2AQueryTaskTool(cfg *config.Config, liveness domain.JobLivenessReporter) *A2AQueryTaskTool {
	return &A2AQueryTaskTool{
		config: cfg,
		formatter: domain.NewCustomFormatter("A2A_QueryTask", func(key string) bool {
			return key == "metadata"
		}),
		liveness: liveness,
	}
}

func (t *A2AQueryTaskTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.A2AQueryTask.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "A2A_QueryTask",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"agent_url": map[string]any{
						"type":        "string",
						"description": "URL of the A2A agent server",
					},
					"context_id": map[string]any{
						"type":        "string",
						"description": "Context ID for the task",
					},
					"task_id": map[string]any{
						"type":        "string",
						"description": "ID of the task to query",
					},
				},
				"required": []string{"agent_url", "context_id", "task_id"},
			},
		},
	}
}

func (t *A2AQueryTaskTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.IsEnabled() {
		return &domain.ToolExecutionResult{
			ToolName:  "A2A_QueryTask",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: A2AQueryTaskResult{
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

	cfg := client.DefaultConfig(agentURL)
	cfg.Transport = telemetry.PropagationTransport(nil)
	adkClient := client.NewClientWithConfig(cfg)
	queryParams := adk.TaskQueryParams{ID: taskID}
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

	result := A2AQueryTaskResult{
		AgentName: agentURL,
		ContextID: contextID,
		TaskID:    taskID,
		Task:      &task,
		Success:   true,
		Duration:  time.Since(startTime),
	}

	result.Message = fmt.Sprintf("Task %s is %s", taskID, task.Status.State)

	return &domain.ToolExecutionResult{
		ToolName:  "A2A_QueryTask",
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
func (t *A2AQueryTaskTool) buildPollingBlockedError(agentURL string) string {
	return fmt.Sprintf("Cannot query task manually - background polling is active for agent %s. The A2A_SubmitTask tool is already polling for updates automatically. Please wait for the polling to complete.", agentURL)
}

func (t *A2AQueryTaskTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "A2A_QueryTask",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: A2AQueryTaskResult{
			Success: false,
			Message: errorMsg,
		},
	}, nil
}

func (t *A2AQueryTaskTool) Validate(args map[string]any) error {
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

func (t *A2AQueryTaskTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

func (t *A2AQueryTaskTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Data == nil {
		return t.formatter.FormatExpanded(result, "")
	}

	queryResult, ok := result.Data.(A2AQueryTaskResult)
	if !ok || queryResult.Task == nil {
		return t.formatter.FormatExpanded(result, t.formatter.FormatAsJSON(result.Data))
	}

	var body strings.Builder
	task := queryResult.Task
	fmt.Fprintf(&body, "Task Status: %s\n", task.Status.State)

	if isFailedTaskState(task.Status.State) {
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

func (t *A2AQueryTaskTool) FormatForUI(result *domain.ToolExecutionResult) string {
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

func (t *A2AQueryTaskTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(A2AQueryTaskResult); ok {
		return fmt.Sprintf("A2A Query Task: %s", data.Message)
	}

	return "A2A query task operation completed"
}

func (t *A2AQueryTaskTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

func (t *A2AQueryTaskTool) ShouldAlwaysExpand() bool {
	return false
}

func (t *A2AQueryTaskTool) IsEnabled() bool {
	return t.config.IsA2AToolsEnabled() && t.config.A2A.Tools.QueryTask.Enabled
}
