package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
	Operation   string                   `json:"operation"`
	TaskID      string                   `json:"task_id,omitempty"`
	Status      domain.A2ATaskStatusEnum `json:"status,omitempty"`
	AgentName   string                   `json:"agent_name,omitempty"`
	Message     string                   `json:"message"`
	Result      interface{}              `json:"result,omitempty"`
	Metadata    map[string]string        `json:"metadata,omitempty"`
	Success     bool                     `json:"success"`
	Duration    time.Duration            `json:"duration,omitempty"`
	CreatedAt   time.Time                `json:"created_at,omitempty"`
	CompletedAt *time.Time               `json:"completed_at,omitempty"`
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
	description := "Submit tasks to Agent-to-Agent (A2A) servers for background execution. Supports task submission, status checking, and result collection."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Task",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"description": "The operation to perform",
						"enum":        []string{"submit", "status", "collect", "cancel", "list_agents", "test_connection"},
					},
					"agent_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the A2A agent (required for submit, test_connection)",
					},
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Task ID (required for status, collect, cancel)",
					},
					"task_type": map[string]interface{}{
						"type":        "string",
						"description": "Type of task to submit (required for submit)",
					},
					"task_description": map[string]interface{}{
						"type":        "string",
						"description": "Description of the task (required for submit)",
					},
					"parameters": map[string]interface{}{
						"type":                 "object",
						"description":          "Task parameters (optional for submit)",
						"additionalProperties": true,
					},
					"priority": map[string]interface{}{
						"type":        "integer",
						"description": "Task priority (1-10, optional for submit)",
						"minimum":     1,
						"maximum":     10,
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Task timeout in seconds (optional for submit)",
					},
				},
				"required": []string{"operation"},
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
				Operation: "error",
				Success:   false,
				Message:   "A2A direct connections are disabled",
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
				Operation: "error",
				Success:   false,
				Message:   "A2A direct service not initialized",
			},
		}, nil
	}

	operation, ok := args["operation"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Task",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "operation parameter is required and must be a string",
			Data: A2ATaskResult{
				Operation: "error",
				Success:   false,
				Message:   "Invalid operation parameter",
			},
		}, nil
	}

	switch operation {
	case "submit":
		return t.handleSubmitTask(ctx, args, startTime)
	case "status":
		return t.handleGetTaskStatus(ctx, args, startTime)
	case "collect":
		return t.handleCollectResults(ctx, args, startTime)
	case "cancel":
		return t.handleCancelTask(ctx, args, startTime)
	case "list_agents":
		return t.handleListAgents(ctx, args, startTime)
	case "test_connection":
		return t.handleTestConnection(ctx, args, startTime)
	default:
		return &domain.ToolExecutionResult{
			ToolName:  "Task",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     fmt.Sprintf("unknown operation: %s", operation),
			Data: A2ATaskResult{
				Operation: operation,
				Success:   false,
				Message:   fmt.Sprintf("Unknown operation: %s", operation),
			},
		}, nil
	}
}

// handleSubmitTask handles task submission
func (t *A2ATaskTool) handleSubmitTask(ctx context.Context, args map[string]any, startTime time.Time) (*domain.ToolExecutionResult, error) {
	agentName, ok := args["agent_name"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_name parameter is required for submit operation", "submit")
	}

	taskType, ok := args["task_type"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_type parameter is required for submit operation", "submit")
	}

	taskDescription, ok := args["task_description"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_description parameter is required for submit operation", "submit")
	}

	task := domain.A2ATask{
		Type:        taskType,
		Description: taskDescription,
		Parameters:  make(map[string]interface{}),
	}

	if params, exists := args["parameters"]; exists {
		if paramMap, ok := params.(map[string]interface{}); ok {
			task.Parameters = paramMap
		}
	}

	if priority, exists := args["priority"]; exists {
		if p, ok := priority.(float64); ok {
			task.Priority = int(p)
		}
	}

	if timeout, exists := args["timeout"]; exists {
		if t, ok := timeout.(float64); ok {
			task.Timeout = int(t)
		}
	}

	taskID, err := t.a2aDirectService.SubmitTask(ctx, agentName, task)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to submit task: %v", err), "submit")
	}

	logger.Debug("A2A task submitted via tool", "task_id", taskID, "agent", agentName, "type", taskType)

	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			Operation: "submit",
			TaskID:    taskID,
			Status:    "submitted",
			AgentName: agentName,
			Success:   true,
			Message:   fmt.Sprintf("Task submitted to agent '%s' with ID: %s", agentName, taskID),
			CreatedAt: time.Now(),
		},
	}, nil
}

// handleGetTaskStatus handles task status retrieval
func (t *A2ATaskTool) handleGetTaskStatus(ctx context.Context, args map[string]any, startTime time.Time) (*domain.ToolExecutionResult, error) {
	taskID, ok := args["task_id"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_id parameter is required for status operation", "status")
	}

	status, err := t.a2aDirectService.GetTaskStatus(ctx, taskID)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to get task status: %v", err), "status")
	}

	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			Operation:   "status",
			TaskID:      taskID,
			Status:      status.Status,
			Success:     true,
			Message:     fmt.Sprintf("Task %s status: %s (%.1f%% complete)", taskID, status.Status, status.Progress),
			CreatedAt:   status.CreatedAt,
			CompletedAt: status.CompletedAt,
			Metadata: map[string]string{
				"progress": fmt.Sprintf("%.1f", status.Progress),
				"message":  status.Message,
			},
		},
	}, nil
}

// handleCollectResults handles result collection
func (t *A2ATaskTool) handleCollectResults(ctx context.Context, args map[string]any, startTime time.Time) (*domain.ToolExecutionResult, error) {
	taskID, ok := args["task_id"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_id parameter is required for collect operation", "collect")
	}

	result, err := t.a2aDirectService.CollectResults(ctx, taskID)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to collect results: %v", err), "collect")
	}

	message := fmt.Sprintf("Results collected for task %s", taskID)
	if !result.Success {
		message = fmt.Sprintf("Task %s failed: %s", taskID, result.Error)
	}

	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			Operation:   "collect",
			TaskID:      taskID,
			Status:      "collected",
			Success:     result.Success,
			Message:     message,
			Result:      result.Result,
			Duration:    result.Duration,
			CreatedAt:   result.CreatedAt,
			CompletedAt: &result.CompletedAt,
			Metadata:    result.Metadata,
		},
	}, nil
}

// handleCancelTask handles task cancellation
func (t *A2ATaskTool) handleCancelTask(ctx context.Context, args map[string]any, startTime time.Time) (*domain.ToolExecutionResult, error) {
	taskID, ok := args["task_id"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_id parameter is required for cancel operation", "cancel")
	}

	err := t.a2aDirectService.CancelTask(ctx, taskID)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to cancel task: %v", err), "cancel")
	}

	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			Operation: "cancel",
			TaskID:    taskID,
			Status:    "cancelled",
			Success:   true,
			Message:   fmt.Sprintf("Task %s has been cancelled", taskID),
		},
	}, nil
}

// handleListAgents handles listing active agents
func (t *A2ATaskTool) handleListAgents(ctx context.Context, args map[string]any, startTime time.Time) (*domain.ToolExecutionResult, error) {
	agents, err := t.a2aDirectService.ListActiveAgents()
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to list agents: %v", err), "list_agents")
	}

	agentList := make([]map[string]interface{}, 0, len(agents))
	for name, agent := range agents {
		agentList = append(agentList, map[string]interface{}{
			"name":        name,
			"url":         agent.URL,
			"description": agent.Description,
			"enabled":     agent.Enabled,
			"metadata":    agent.Metadata,
		})
	}

	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			Operation: "list_agents",
			Success:   true,
			Message:   fmt.Sprintf("Found %d active A2A agents", len(agents)),
			Result:    agentList,
		},
	}, nil
}

// handleTestConnection handles connection testing
func (t *A2ATaskTool) handleTestConnection(ctx context.Context, args map[string]any, startTime time.Time) (*domain.ToolExecutionResult, error) {
	agentName, ok := args["agent_name"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_name parameter is required for test_connection operation", "test_connection")
	}

	err := t.a2aDirectService.TestConnection(ctx, agentName)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Connection test failed: %v", err), "test_connection")
	}

	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			Operation: "test_connection",
			AgentName: agentName,
			Success:   true,
			Message:   fmt.Sprintf("Connection to agent '%s' successful", agentName),
		},
	}, nil
}

// errorResult creates an error result
func (t *A2ATaskTool) errorResult(args map[string]any, startTime time.Time, errorMsg, operation string) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: A2ATaskResult{
			Operation: operation,
			Success:   false,
			Message:   errorMsg,
		},
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *A2ATaskTool) Validate(args map[string]any) error {
	operation, ok := args["operation"].(string)
	if !ok {
		return fmt.Errorf("operation parameter is required and must be a string")
	}

	switch operation {
	case "submit":
		if _, ok := args["agent_name"].(string); !ok {
			return fmt.Errorf("agent_name parameter is required for submit operation")
		}
		if _, ok := args["task_type"].(string); !ok {
			return fmt.Errorf("task_type parameter is required for submit operation")
		}
		if _, ok := args["task_description"].(string); !ok {
			return fmt.Errorf("task_description parameter is required for submit operation")
		}
	case "status", "collect", "cancel":
		if _, ok := args["task_id"].(string); !ok {
			return fmt.Errorf("task_id parameter is required for %s operation", operation)
		}
	case "test_connection":
		if _, ok := args["agent_name"].(string); !ok {
			return fmt.Errorf("agent_name parameter is required for test_connection operation")
		}
	case "list_agents":
		// No additional validation needed
	default:
		return fmt.Errorf("unknown operation: %s", operation)
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
	result := fmt.Sprintf("A2A Task %s: %s", data.Operation, data.Message)

	if data.TaskID != "" {
		result += fmt.Sprintf(" (Task ID: %s)", data.TaskID)
	}

	if data.Result != nil {
		if jsonData, err := json.Marshal(data.Result); err == nil {
			result += fmt.Sprintf(" Result: %s", string(jsonData))
		}
	}

	return result
}

// formatForUI formats the result for UI display
func (t *A2ATaskTool) formatForUI(data A2ATaskResult) string {
	result := fmt.Sprintf("**A2A %s**: %s", data.Operation, data.Message)

	if data.TaskID != "" {
		result += fmt.Sprintf("\n📋 **Task ID**: `%s`", data.TaskID)
	}

	if data.AgentName != "" {
		result += fmt.Sprintf("\n🤖 **Agent**: %s", data.AgentName)
	}

	if data.Status != "" {
		result += fmt.Sprintf("\n📊 **Status**: %s", data.Status)
	}

	if data.Duration > 0 {
		result += fmt.Sprintf("\n⏱️ **Duration**: %v", data.Duration)
	}

	if data.Result != nil && data.Operation == "collect" {
		if jsonData, err := json.Marshal(data.Result); err == nil {
			result += fmt.Sprintf("\n📄 **Result**: ```json\n%s\n```", string(jsonData))
		}
	}

	return result
}

// FormatPreview returns a short preview of the result for UI display
func (t *A2ATaskTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(A2ATaskResult); ok {
		return fmt.Sprintf("A2A %s: %s", data.Operation, data.Message)
	}

	return "A2A task operation completed"
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *A2ATaskTool) ShouldCollapseArg(key string) bool {
	return key == "parameters"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *A2ATaskTool) ShouldAlwaysExpand() bool {
	return false
}
