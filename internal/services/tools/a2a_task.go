package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// A2ATaskTool handles A2A task submission and management
type A2ATaskTool struct {
	config           *config.Config
	a2aDirectService domain.A2ADirectService
}

// A2ATaskResult represents the result of an A2A task operation
type A2ATaskResult struct {
	AgentName string        `json:"agent_name"`
	Task      *adk.Task     `json:"task"`
	Success   bool          `json:"success"`
	Message   string        `json:"message"`
	Duration  time.Duration `json:"duration"`
}

// NewA2ATaskTool creates a new A2A task tool
func NewA2ATaskTool(cfg *config.Config, a2aDirectService domain.A2ADirectService) *A2ATaskTool {
	return &A2ATaskTool{
		config:           cfg,
		a2aDirectService: a2aDirectService,
	}
}

// Definition returns the tool definition for the LLM
func (t *A2ATaskTool) Definition() sdk.ChatCompletionTool {
	description := "Submit a task to an Agent-to-Agent (A2A) server for execution."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Task",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_url": map[string]interface{}{
						"type":        "string",
						"description": "URL of the A2A agent",
					},
					"task_description": map[string]interface{}{
						"type":        "string",
						"description": "Description of the task",
					},
				},
				"required": []string{"agent_url", "task_description"},
			},
		},
	}
}

// Execute runs the tool with given arguments
func (t *A2ATaskTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.config.IsA2ADirectEnabled() {
		return &domain.ToolExecutionResult{
			ToolName:  "Task",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A direct connections are disabled in configuration",
			Data: A2ATaskResult{
				Success: false,
				Message: "A2A direct connections are disabled",
			},
		}, nil
	}

	if t.a2aDirectService == nil {
		return &domain.ToolExecutionResult{
			ToolName:  "Task",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A direct service not available",
			Data: A2ATaskResult{
				Success: false,
				Message: "A2A direct service not initialized",
			},
		}, nil
	}

	agentURL, ok := args["agent_url"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_url parameter is required and must be a string")
	}

	taskDescription, ok := args["task_description"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_description parameter is required and must be a string")
	}

	// Create ADK Task structure
	adkTask := adk.Task{
		ID:   uuid.New().String(),
		Kind: "query", // Default kind for A2A tasks
		Metadata: map[string]any{
			"description": taskDescription,
		},
		Status: adk.TaskStatus{
			State: adk.TaskStateSubmitted,
		},
	}

	// Add optional metadata
	if metadata, exists := args["metadata"]; exists {
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			for k, v := range metadataMap {
				adkTask.Metadata[k] = v
			}
		}
	}

	resultTask, err := t.a2aDirectService.SubmitTask(ctx, agentURL, adkTask)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to submit task: %v", err))
	}

	logger.Debug("A2A task submitted via tool", "task_id", resultTask.ID, "agent_url", agentURL)

	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			AgentName: agentURL,
			Task:      resultTask,
			Success:   true,
			Message:   fmt.Sprintf("Task submitted to agent at %s with ID: %s", agentURL, resultTask.ID),
			Duration:  time.Since(startTime),
		},
	}, nil
}

// errorResult creates an error result
func (t *A2ATaskTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: A2ATaskResult{
			Success: false,
			Message: errorMsg,
		},
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *A2ATaskTool) Validate(args map[string]any) error {
	if _, ok := args["agent_url"].(string); !ok {
		return fmt.Errorf("agent_url parameter is required and must be a string")
	}
	if _, ok := args["task_description"].(string); !ok {
		return fmt.Errorf("task_description parameter is required and must be a string")
	}
	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *A2ATaskTool) IsEnabled() bool {
	return t.config.IsA2ADirectEnabled()
}

// FormatResult formats tool execution results for different contexts
func (t *A2ATaskTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	if result.Data == nil {
		return result.Error
	}

	data, ok := result.Data.(A2ATaskResult)
	if !ok {
		return "Invalid A2A task result format"
	}

	switch formatType {
	case domain.FormatterLLM:
		return t.formatForLLM(data)
	case domain.FormatterShort:
		return data.Message
	default:
		return t.formatForUI(data)
	}
}

// formatForLLM formats the result for LLM consumption
func (t *A2ATaskTool) formatForLLM(data A2ATaskResult) string {
	result := fmt.Sprintf("A2A Task: %s", data.Message)

	if data.Task != nil {
		result += fmt.Sprintf(" (Task ID: %s)", data.Task.ID)
	}

	return result
}

// formatForUI formats the result for UI display
func (t *A2ATaskTool) formatForUI(data A2ATaskResult) string {
	result := fmt.Sprintf("**A2A Task**: %s", data.Message)

	if data.Task != nil {
		result += fmt.Sprintf("\n📋 **Task ID**: `%s`", data.Task.ID)
		result += fmt.Sprintf("\n📝 **Kind**: %s", data.Task.Kind)
	}

	if data.AgentName != "" {
		result += fmt.Sprintf("\n🤖 **Agent**: %s", data.AgentName)
	}

	if data.Duration > 0 {
		result += fmt.Sprintf("\n⏱️ **Duration**: %v", data.Duration)
	}

	return result
}

// FormatPreview returns a short preview of the result for UI display
func (t *A2ATaskTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(A2ATaskResult); ok {
		return fmt.Sprintf("A2A Task: %s", data.Message)
	}

	return "A2A task operation completed"
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *A2ATaskTool) ShouldCollapseArg(key string) bool {
	return key == "metadata"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *A2ATaskTool) ShouldAlwaysExpand() bool {
	return false
}
