package tools

import (
	"context"
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
	config    *config.Config
	formatter domain.CustomFormatter
}

type A2AQueryTaskResult struct {
	AgentName string        `json:"agent_name"`
	ContextID string        `json:"context_id"`
	TaskID    string        `json:"task_id"`
	Status    string        `json:"status"`
	Result    string        `json:"result,omitempty"`
	Success   bool          `json:"success"`
	Message   string        `json:"message"`
	Duration  time.Duration `json:"duration"`
}

func NewA2AQueryTaskTool(cfg *config.Config) *A2AQueryTaskTool {
	return &A2AQueryTaskTool{
		config: cfg,
		formatter: domain.NewCustomFormatter("QueryTask", func(key string) bool {
			return key == "metadata"
		}),
	}
}

func (t *A2AQueryTaskTool) Definition() sdk.ChatCompletionTool {
	description := "Query the status and result of a specific A2A task. Returns task status and last message if completed/input-required, or current status if still working."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "QueryTask",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_url": map[string]interface{}{
						"type":        "string",
						"description": "URL of the A2A agent server",
					},
					"context_id": map[string]interface{}{
						"type":        "string",
						"description": "Context ID for the task",
					},
					"task_id": map[string]interface{}{
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
			ToolName:  "QueryTask",
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

	adkClient := client.NewClient(agentURL)
	queryParams := adk.TaskQueryParams{ID: taskID}
	taskResponse, err := adkClient.GetTask(ctx, queryParams)
	if err != nil {
		logger.Error("Failed to query task", "agent_url", agentURL, "task_id", taskID, "error", err)
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to query task: %v", err))
	}

	var task adk.Task
	if err := mapToStruct(taskResponse.Result, &task); err != nil {
		return t.errorResult(args, startTime, "Failed to parse task response")
	}

	result := A2AQueryTaskResult{
		AgentName: agentURL,
		ContextID: contextID,
		TaskID:    taskID,
		Status:    string(task.Status.State),
		Success:   true,
		Duration:  time.Since(startTime),
	}

	if task.Status.State == adk.TaskStateCompleted || task.Status.State == "input-required" {
		if task.Status.Message != nil {
			result.Result = extractTextFromParts(task.Status.Message.Parts)
		}
		result.Message = fmt.Sprintf("Task %s is %s", taskID, task.Status.State)
	} else {
		result.Message = fmt.Sprintf("Task %s is %s", taskID, task.Status.State)
	}

	return &domain.ToolExecutionResult{
		ToolName:  "QueryTask",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data:      result,
	}, nil
}

func (t *A2AQueryTaskTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "QueryTask",
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

	if result.Data != nil {
		dataContent := t.formatter.FormatAsJSON(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

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
	return t.config.Tools.QueryTask.Enabled
}
