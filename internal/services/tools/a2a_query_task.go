package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

type A2AQueryTaskTool struct {
	config      *config.Config
	formatter   domain.CustomFormatter
	taskTracker domain.TaskTracker
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

func NewA2AQueryTaskTool(cfg *config.Config, taskTracker domain.TaskTracker) *A2AQueryTaskTool {
	return &A2AQueryTaskTool{
		config: cfg,
		formatter: domain.NewCustomFormatter("A2A_QueryTask", func(key string) bool {
			return key == "metadata"
		}),
		taskTracker: taskTracker,
	}
}

func (t *A2AQueryTaskTool) Definition() sdk.ChatCompletionTool {
	description := "Query the status and result of a specific A2A task. Returns the complete task object including status, artifacts, and message data. IMPORTANT: When you submit a task via A2A_SubmitTask, it automatically monitors the task in the background and emits an event when complete - you will be notified automatically. DO NOT manually query recently submitted tasks during background monitoring. Only use this tool to: 1) Check tasks from previous conversations, 2) Check tasks submitted outside this session, or 3) Get detailed results AFTER you receive a completion notification."
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

	if t.taskTracker != nil && t.taskTracker.IsPolling(agentURL) {
		state := t.taskTracker.GetPollingState(agentURL)
		errorMsg := t.buildPollingBlockedError(agentURL, state)
		return t.errorResult(args, startTime, errorMsg)
	}

	adkClient := client.NewClient(agentURL)
	queryParams := adk.TaskQueryParams{ID: taskID}
	taskResponse, err := adkClient.GetTask(ctx, queryParams)
	if err != nil {
		logger.Error("Failed to query task", "agent_url", agentURL, "task_id", taskID, "error", err)
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

func (t *A2AQueryTaskTool) buildPollingBlockedError(agentURL string, state *domain.TaskPollingState) string {
	if state == nil || state.NextPollTime.IsZero() {
		return fmt.Sprintf("Cannot query task manually - background polling is active for agent %s. The A2A_SubmitTask tool is already polling for updates automatically. Please wait for the polling to complete.", agentURL)
	}

	timeUntilNextPoll := time.Until(state.NextPollTime)
	if timeUntilNextPoll > 0 {
		return fmt.Sprintf("Cannot query task manually - background polling is active for agent %s. Next automatic poll in %.1f seconds (interval: %v). The A2A_SubmitTask tool is already polling for updates. Wait for the next poll to complete before querying manually.",
			agentURL, timeUntilNextPoll.Seconds(), state.CurrentInterval)
	}

	return fmt.Sprintf("Cannot query task manually - background polling is active for agent %s. Next automatic poll is happening now (interval: %v). The A2A_SubmitTask tool is already polling for updates. Wait for it to complete.",
		agentURL, state.CurrentInterval)
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

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data == nil {
		hasDataSection := false
		output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))
		return output.String()
	}

	queryResult, ok := result.Data.(A2AQueryTaskResult)
	if !ok || queryResult.Task == nil {
		dataContent := t.formatter.FormatAsJSON(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
		hasDataSection := result.Data != nil
		output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))
		return output.String()
	}

	task := queryResult.Task
	output.WriteString("Task Status: " + string(task.Status.State) + "\n")

	hasArtifacts := len(task.Artifacts) > 0
	if !hasArtifacts {
		output.WriteString("\nNo artifacts available for this task.\n")
		output.WriteString("\nFull Task Data:\n")
		dataContent := t.formatter.FormatAsJSON(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
		hasDataSection := result.Data != nil
		output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))
		return output.String()
	}

	output.WriteString(fmt.Sprintf("\nArtifacts (%d):\n", len(task.Artifacts)))
	for i, artifact := range task.Artifacts {
		output.WriteString(fmt.Sprintf("%d. ", i+1))
		if artifact.Name != nil {
			output.WriteString(fmt.Sprintf("Name: %s", *artifact.Name))
		}
		output.WriteString(fmt.Sprintf(" (ID: %s)", artifact.ArtifactID))

		hasMetadata := artifact.Metadata != nil
		if hasMetadata {
			if url, ok := artifact.Metadata["url"].(string); ok {
				output.WriteString(fmt.Sprintf("\n   Download URL: %s", url))
			}
			if mimeType, ok := artifact.Metadata["mime_type"].(string); ok {
				output.WriteString(fmt.Sprintf("\n   MIME Type: %s", mimeType))
			}
			if size, ok := artifact.Metadata["size"].(float64); ok {
				output.WriteString(fmt.Sprintf("\n   Size: %d bytes", int64(size)))
			}
		}
		output.WriteString("\n")
	}

	output.WriteString("\nFull Task Data:\n")
	dataContent := t.formatter.FormatAsJSON(result.Data)
	hasMetadata := len(result.Metadata) > 0
	output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

func (t *A2AQueryTaskTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))

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
