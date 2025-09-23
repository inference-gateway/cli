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
	sdk "github.com/inference-gateway/sdk"
)

// A2ATaskTool handles A2A task submission and management
type A2ATaskTool struct {
	config      *config.Config
	formatter   domain.CustomFormatter
	taskTracker domain.TaskTracker
	client      client.A2AClient
}

// A2ATaskResult represents the result of an A2A task operation
type A2ATaskResult struct {
	AgentName  string        `json:"agent_name"`
	Task       *adk.Task     `json:"task"`
	Success    bool          `json:"success"`
	Message    string        `json:"message"`
	Duration   time.Duration `json:"duration"`
	TaskResult string        `json:"task_result"`
}

// NewA2ATaskTool creates a new A2A task tool
func NewA2ATaskTool(cfg *config.Config, taskTracker domain.TaskTracker) *A2ATaskTool {
	return &A2ATaskTool{
		config:      cfg,
		taskTracker: taskTracker,
		client:      nil,
		formatter: domain.NewCustomFormatter("A2A_Task", func(key string) bool {
			return key == "metadata" || key == "task_description"
		}),
	}
}

// NewA2ATaskToolWithClient creates a new A2A task tool with an injected client (for testing)
func NewA2ATaskToolWithClient(cfg *config.Config, taskTracker domain.TaskTracker, client client.A2AClient) *A2ATaskTool {
	return &A2ATaskTool{
		config:      cfg,
		taskTracker: taskTracker,
		client:      client,
		formatter: domain.NewCustomFormatter("A2A_Task", func(key string) bool {
			return key == "metadata" || key == "task_description"
		}),
	}
}

// Definition returns the tool definition for the LLM
func (t *A2ATaskTool) Definition() sdk.ChatCompletionTool {
	description := "Submit work to an A2A agent server: ask questions, execute tasks, perform actions, or continue existing work. Use this for ANY interaction where you need an agent to respond with answers or complete work. The Query tool is ONLY for retrieving agent metadata/capabilities (agent card)."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "A2A_Task",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_url": map[string]interface{}{
						"type":        "string",
						"description": "URL of the A2A agent server",
					},
					"task_description": map[string]interface{}{
						"type":        "string",
						"description": "The question to ask or work to perform. Can be a question, task, action, or continuation of existing work",
					},
				},
				"required": []string{"agent_url", "task_description"},
			},
		},
	}
}

// Execute runs the tool with given arguments
func (t *A2ATaskTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) { // nolint:gocyclo,cyclop,funlen,gocognit
	startTime := time.Now()

	if !t.IsEnabled() {
		return &domain.ToolExecutionResult{
			ToolName:  "A2A_Task",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: A2ATaskResult{
				Success: false,
				Message: "A2A connections are disabled",
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

	var existingTaskID string
	var contextID string
	if t.taskTracker != nil {
		existingTaskID = t.taskTracker.GetFirstTaskID()
		contextID = t.taskTracker.GetContextID()
	}

	adkTask := adk.Task{
		Kind: "task",
		Metadata: map[string]any{
			"description": taskDescription,
		},
		Status: adk.TaskStatus{
			State: adk.TaskStateSubmitted,
		},
	}

	if metadata, exists := args["metadata"]; exists {
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			for k, v := range metadataMap {
				adkTask.Metadata[k] = v
			}
		}
	}

	var adkClient client.A2AClient
	if t.client != nil {
		adkClient = t.client
	} else {
		adkClient = client.NewClient(agentURL)
	}
	message := adk.Message{
		Kind: "message",
		Role: "user",
		Parts: []adk.Part{
			map[string]any{
				"kind": "text",
				"text": taskDescription,
			},
		},
	}

	if existingTaskID != "" {
		message.TaskID = &existingTaskID
	}

	if contextID != "" {
		message.ContextID = &contextID
	}

	msgParams := adk.MessageSendParams{
		Message: message,
		Configuration: &adk.MessageSendConfiguration{
			Blocking:            &[]bool{true}[0],
			AcceptedOutputModes: []string{"text"},
		},
	}

	var finalResult string
	var taskID string

	taskResponse, err := adkClient.SendTask(ctx, msgParams)
	if err != nil {
		if t.taskTracker != nil && existingTaskID != "" && t.isTaskNotFoundError(err) {
			t.taskTracker.ClearTaskID()
			return t.errorResult(args, startTime, fmt.Sprintf("Previous task no longer exists (cleared from tracker): %v", err))
		}
		return t.errorResult(args, startTime, fmt.Sprintf("A2A task submission failed: %v", err))
	}

	var submittedTask adk.Task
	if err := mapToStruct(taskResponse.Result, &submittedTask); err != nil {
		return t.errorResult(args, startTime, "Failed to parse task submission response")
	}

	if submittedTask.ID == "" {
		return t.errorResult(args, startTime, "Task submitted but no task ID received")
	}

	taskID = submittedTask.ID

	if submittedTask.ContextID != "" {
		contextID = submittedTask.ContextID
		if t.taskTracker != nil {
			t.taskTracker.SetContextID(submittedTask.ContextID)
		}
	}

	if t.taskTracker != nil && existingTaskID != "" &&
		(submittedTask.Status.State == adk.TaskStateCompleted || submittedTask.Status.State == adk.TaskStateFailed) {
		t.taskTracker.ClearTaskID()
		return t.errorResult(args, startTime, fmt.Sprintf("Previous task %s is already %s (cleared from tracker)", existingTaskID, submittedTask.Status.State))
	}

	if t.taskTracker != nil && existingTaskID == "" {
		t.taskTracker.SetFirstTaskID(taskID)
	}

	maxAttempts := 60
	pollInterval := time.Duration(t.config.A2A.Task.StatusPollSeconds) * time.Second

	for attempt := range maxAttempts {
		select {
		case <-ctx.Done():
			return t.errorResult(args, startTime, "Task cancelled")
		default:
		}

		if attempt > 0 {
			select {
			case <-ctx.Done():
				return t.errorResult(args, startTime, "Task cancelled")
			case <-time.After(pollInterval):
			}
		}

		queryParams := adk.TaskQueryParams{ID: taskID}
		taskStatus, err := adkClient.GetTask(ctx, queryParams)
		if err != nil {
			continue
		}

		var currentTask adk.Task
		if err := mapToStruct(taskStatus.Result, &currentTask); err != nil {
			continue
		}

		if currentTask.Status.State == adk.TaskStateCompleted || currentTask.Status.State == adk.TaskStateFailed {
			finalResult += t.extractTaskResult(currentTask)
			break
		}

		if currentTask.Status.State == "input-required" {
			return t.handleInputRequiredState(args, agentURL, taskID, currentTask, adkTask, startTime)
		}
	}

	adkTask.Status.State = adk.TaskStateCompleted
	if finalResult != "" {
		adkTask.Metadata["result"] = finalResult
	}
	if taskID != "" {
		adkTask.ID = taskID
	}
	if contextID != "" {
		adkTask.ContextID = contextID
	}

	return &domain.ToolExecutionResult{
		ToolName:  "A2A_Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			AgentName:  agentURL,
			Task:       &adkTask,
			Success:    true,
			Message:    fmt.Sprintf("A2A task submitted to %s", agentURL),
			Duration:   time.Since(startTime),
			TaskResult: finalResult,
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
	return t.config.IsA2AToolsEnabled() && t.config.A2A.Tools.Task.Enabled
}

// FormatResult formats tool execution results for different contexts
func (t *A2ATaskTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *A2ATaskTool) FormatForLLM(result *domain.ToolExecutionResult) string {
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

// FormatForUI formats the result for UI display
func (t *A2ATaskTool) FormatForUI(result *domain.ToolExecutionResult) string {
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

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *A2ATaskTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *A2ATaskTool) ShouldAlwaysExpand() bool {
	return false
}

// errorResult creates an error result
func (t *A2ATaskTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "A2A_Task",
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

// extractTextFromParts extracts text content from message parts
func extractTextFromParts(parts []adk.Part) string {
	var result strings.Builder
	for _, part := range parts {
		if partMap, ok := part.(map[string]any); ok {
			if text, exists := partMap["text"]; exists {
				if textStr, ok := text.(string); ok {
					result.WriteString(textStr)
				}
			}
		}
	}
	return result.String()
}

// extractTaskResult extracts the result text from a completed or failed task
func (t *A2ATaskTool) extractTaskResult(task adk.Task) string {
	if task.Status.Message != nil {
		return extractTextFromParts(task.Status.Message.Parts)
	}
	return ""
}

// handleInputRequiredState handles the input-required task state
func (t *A2ATaskTool) handleInputRequiredState(args map[string]any, agentURL, taskID string, currentTask adk.Task, adkTask adk.Task, startTime time.Time) (*domain.ToolExecutionResult, error) {
	inputMessage := ""
	if currentTask.Status.Message != nil {
		inputMessage = extractTextFromParts(currentTask.Status.Message.Parts)
	}

	if inputMessage == "" {
		inputMessage = "Input required"
	}

	adkTask.Status.State = "input-required"
	adkTask.Metadata["input_required"] = inputMessage
	adkTask.ID = taskID

	return &domain.ToolExecutionResult{
		ToolName:  "A2A_Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			AgentName:  agentURL,
			Task:       &adkTask,
			Success:    true,
			Message:    fmt.Sprintf("Task %s requires input: %s", taskID, inputMessage),
			Duration:   time.Since(startTime),
			TaskResult: inputMessage,
		},
	}, nil
}

// isTaskNotFoundError checks if the error indicates a task was not found
func (t *A2ATaskTool) isTaskNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errorStr := strings.ToLower(err.Error())
	return strings.Contains(errorStr, "task not found") ||
		strings.Contains(errorStr, "not found") ||
		strings.Contains(errorStr, "32603")
}

// mapToStruct converts a map[string]any to a struct using JSON marshaling
func mapToStruct(data any, target any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, target)
}
