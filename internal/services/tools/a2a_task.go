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

// A2ASubmitTaskTool handles A2A task submission and management
type A2ASubmitTaskTool struct {
	config      *config.Config
	formatter   domain.CustomFormatter
	taskTracker domain.TaskTracker
	client      client.A2AClient
}

// A2ASubmitTaskResult represents the result of an A2A task operation
type A2ASubmitTaskResult struct {
	AgentName   string        `json:"agent_name"`
	Task        *adk.Task     `json:"task"`
	Success     bool          `json:"success"`
	Message     string        `json:"message"`
	Duration    time.Duration `json:"duration"`
	TaskResult  string        `json:"task_result"`
	TaskType    string        `json:"task_type"`
	TimedOut    bool          `json:"timed_out"`
	WentIdle    bool          `json:"went_idle"`
	IdleTimeout time.Duration `json:"idle_timeout,omitempty"`
}

// NewA2ASubmitTaskTool creates a new A2A task tool
func NewA2ASubmitTaskTool(cfg *config.Config, taskTracker domain.TaskTracker) *A2ASubmitTaskTool {
	return &A2ASubmitTaskTool{
		config:      cfg,
		taskTracker: taskTracker,
		client:      nil,
		formatter: domain.NewCustomFormatter("A2A_SubmitTask", func(key string) bool {
			return key == "metadata" || key == "task_description"
		}),
	}
}

// NewA2ASubmitTaskToolWithClient creates a new A2A task tool with an injected client (for testing)
func NewA2ASubmitTaskToolWithClient(cfg *config.Config, taskTracker domain.TaskTracker, client client.A2AClient) *A2ASubmitTaskTool {
	return &A2ASubmitTaskTool{
		config:      cfg,
		taskTracker: taskTracker,
		client:      client,
		formatter: domain.NewCustomFormatter("A2A_SubmitTask", func(key string) bool {
			return key == "metadata" || key == "task_description"
		}),
	}
}

func (t *A2ASubmitTaskTool) validateExistingTask(ctx context.Context, adkClient client.A2AClient, existingTaskID, agentURL string, args map[string]any, startTime time.Time) *domain.ToolExecutionResult {
	if existingTaskID == "" {
		return nil
	}

	queryParams := adk.TaskQueryParams{ID: existingTaskID}
	taskStatus, err := adkClient.GetTask(ctx, queryParams)
	if err != nil {
		return nil
	}

	if taskStatus == nil {
		return nil
	}

	var existingTask adk.Task
	if err := mapToStruct(taskStatus.Result, &existingTask); err != nil {
		return nil
	}

	if existingTask.Status.State != adk.TaskStateWorking {
		return nil
	}

	result, _ := t.errorResult(args, startTime, fmt.Sprintf("Cannot create new task: existing task %s is still in working state on agent %s. Wait for it to complete or use A2A_QueryTask to check status.", existingTaskID, agentURL))
	return result
}

// Definition returns the tool definition for the LLM
func (t *A2ASubmitTaskTool) Definition() sdk.ChatCompletionTool {
	description := "Submit work to an A2A agent server: ask questions, execute tasks, perform actions, or continue existing work. Use this for ANY interaction where you need an agent to respond with answers or complete work. The Query tool is ONLY for retrieving agent metadata/capabilities (agent card)."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "A2A_SubmitTask",
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
//nolint:gocyclo,cyclop,funlen
func (t *A2ASubmitTaskTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.IsEnabled() {
		return &domain.ToolExecutionResult{
			ToolName:  "A2A_SubmitTask",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: A2ASubmitTaskResult{
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

	if t.taskTracker != nil && t.taskTracker.IsPolling(agentURL) {
		return t.errorResult(args, startTime, fmt.Sprintf("Cannot create new task: a task is already being polled for agent %s. Wait for it to complete.", agentURL))
	}

	var existingTaskID string
	var contextID string
	if t.taskTracker != nil {
		existingTaskID = t.taskTracker.GetTaskIDForAgent(agentURL)
		contextID = t.taskTracker.GetContextIDForAgent(agentURL)
	}

	var adkClient client.A2AClient
	if t.client != nil {
		adkClient = t.client
	} else {
		adkClient = client.NewClient(agentURL)
	}

	if result := t.validateExistingTask(ctx, adkClient, existingTaskID, agentURL, args, startTime); result != nil {
		return result, nil
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

	taskResponse, err := adkClient.SendTask(ctx, msgParams)
	if err != nil {
		shouldClear := t.taskTracker != nil && existingTaskID != "" && t.isTaskNotFoundError(err)
		if shouldClear {
			t.taskTracker.ClearTaskIDForAgent(agentURL)
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

	taskID := submittedTask.ID

	if submittedTask.ContextID != "" && t.taskTracker != nil {
		contextID = submittedTask.ContextID
		t.taskTracker.SetContextIDForAgent(agentURL, submittedTask.ContextID)
	}

	isCompleted := submittedTask.Status.State == adk.TaskStateCompleted
	isFailed := submittedTask.Status.State == adk.TaskStateFailed
	if t.taskTracker != nil && existingTaskID != "" && (isCompleted || isFailed) {
		t.taskTracker.ClearTaskIDForAgent(agentURL)
		return t.errorResult(args, startTime, fmt.Sprintf("Previous task %s is already %s (cleared from tracker)", existingTaskID, submittedTask.Status.State))
	}

	if t.taskTracker != nil && existingTaskID == "" {
		t.taskTracker.SetTaskIDForAgent(agentURL, taskID)
	}

	pollCtx, cancel := context.WithCancel(context.Background())
	pollingState := &domain.TaskPollingState{
		TaskID:     taskID,
		AgentURL:   agentURL,
		IsPolling:  false,
		StartedAt:  time.Now(),
		LastPollAt: time.Now(),
		CancelFunc: cancel,
		ResultChan: make(chan *domain.ToolExecutionResult, 1),
		ErrorChan:  make(chan error, 1),
		StatusChan: make(chan *domain.A2ATaskStatusUpdate, 10),
	}

	if t.taskTracker != nil {
		t.taskTracker.StartPolling(agentURL, pollingState)
	}

	go t.pollTaskInBackground(pollCtx, agentURL, taskID, pollingState)

	adkTask := adk.Task{
		Kind: "task",
		ID:   taskID,
		Metadata: map[string]any{
			"description": taskDescription,
		},
		Status: adk.TaskStatus{
			State: adk.TaskStateSubmitted,
		},
	}

	if contextID != "" {
		adkTask.ContextID = contextID
	}

	if metadata, exists := args["metadata"]; exists {
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			for k, v := range metadataMap {
				adkTask.Metadata[k] = v
			}
		}
	}

	return &domain.ToolExecutionResult{
		ToolName:  "A2A_SubmitTask",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ASubmitTaskResult{
			AgentName:  agentURL,
			Task:       &adkTask,
			Success:    true,
			Message:    fmt.Sprintf("Task submitted to %s (polling in background)", agentURL),
			Duration:   time.Since(startTime),
			TaskType:   taskDescription,
			TaskResult: fmt.Sprintf("Task %s submitted successfully. Polling for completion in background.", taskID),
		},
	}, nil
}

// pollTaskInBackground polls for task completion in a background goroutine
//nolint:gocognit
func (t *A2ASubmitTaskTool) pollTaskInBackground(
	ctx context.Context,
	agentURL string,
	taskID string,
	state *domain.TaskPollingState,
) {
	defer func() {
		if t.taskTracker != nil {
			t.taskTracker.StopPolling(agentURL)
		}
	}()

	var adkClient client.A2AClient
	if t.client != nil {
		adkClient = t.client
	} else {
		adkClient = client.NewClient(agentURL)
	}

	pollInterval := time.Duration(t.config.A2A.Task.StatusPollSeconds) * time.Second
	idleTimeout := time.Duration(t.config.A2A.Task.IdleTimeoutSec) * time.Second
	deadline := time.Now().Add(idleTimeout)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if state.ErrorChan != nil {
				state.ErrorChan <- fmt.Errorf("task cancelled")
			}
			return

		case <-ticker.C:
			if time.Now().After(deadline) {
				result := &domain.ToolExecutionResult{
					ToolName: "A2A_SubmitTask",
					Success:  true,
					Duration: time.Since(state.StartedAt),
					Data: A2ASubmitTaskResult{
						AgentName:   agentURL,
						Success:     true,
						Message:     fmt.Sprintf("Task went idle after %v", idleTimeout),
						WentIdle:    true,
						IdleTimeout: idleTimeout,
					},
				}
				if state.ResultChan != nil {
					state.ResultChan <- result
				}
				return
			}

			state.LastPollAt = time.Now()

			queryParams := adk.TaskQueryParams{ID: taskID}
			taskStatus, err := adkClient.GetTask(ctx, queryParams)
			if err != nil {
				continue
			}

			var currentTask adk.Task
			if err := mapToStruct(taskStatus.Result, &currentTask); err != nil {
				continue
			}

			if state.StatusChan != nil {
				statusMessage := ""
				if currentTask.Status.Message != nil {
					statusMessage = extractTextFromParts(currentTask.Status.Message.Parts)
				}
				statusUpdate := &domain.A2ATaskStatusUpdate{
					TaskID:    taskID,
					AgentURL:  agentURL,
					State:     string(currentTask.Status.State),
					Message:   statusMessage,
					Timestamp: time.Now(),
				}
				select {
				case state.StatusChan <- statusUpdate:
				default:
				}
			}

			switch currentTask.Status.State {
			case adk.TaskStateCompleted:
				finalResult := t.extractTaskResult(currentTask)
				result := &domain.ToolExecutionResult{
					ToolName: "A2A_SubmitTask",
					Success:  true,
					Duration: time.Since(state.StartedAt),
					Data: A2ASubmitTaskResult{
						AgentName:  agentURL,
						Success:    true,
						Message:    fmt.Sprintf("Task %s", currentTask.Status.State),
						Duration:   time.Since(state.StartedAt),
						TaskResult: finalResult,
					},
				}
				if state.ResultChan != nil {
					state.ResultChan <- result
				}
				return

			case adk.TaskStateFailed:
				finalResult := t.extractTaskResult(currentTask)
				result := &domain.ToolExecutionResult{
					ToolName: "A2A_SubmitTask",
					Success:  false,
					Duration: time.Since(state.StartedAt),
					Data: A2ASubmitTaskResult{
						AgentName:  agentURL,
						Success:    false,
						Message:    fmt.Sprintf("Task %s", currentTask.Status.State),
						Duration:   time.Since(state.StartedAt),
						TaskResult: finalResult,
					},
				}
				if state.ResultChan != nil {
					state.ResultChan <- result
				}
				return

			case adk.TaskStateInputRequired:
				inputMessage := ""
				if currentTask.Status.Message != nil {
					inputMessage = extractTextFromParts(currentTask.Status.Message.Parts)
				}

				result := &domain.ToolExecutionResult{
					ToolName: "A2A_SubmitTask",
					Success:  true,
					Duration: time.Since(state.StartedAt),
					Data: A2ASubmitTaskResult{
						AgentName:  agentURL,
						Success:    true,
						Message:    fmt.Sprintf("Task requires input: %s", inputMessage),
						TaskResult: inputMessage,
					},
				}
				if state.ResultChan != nil {
					state.ResultChan <- result
				}
				return
			}
		}
	}
}

// Validate checks if the tool arguments are valid
func (t *A2ASubmitTaskTool) Validate(args map[string]any) error {
	if _, ok := args["agent_url"].(string); !ok {
		return fmt.Errorf("agent_url parameter is required and must be a string")
	}
	if _, ok := args["task_description"].(string); !ok {
		return fmt.Errorf("task_description parameter is required and must be a string")
	}
	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *A2ASubmitTaskTool) IsEnabled() bool {
	return t.config.IsA2AToolsEnabled() && t.config.A2A.Tools.SubmitTask.Enabled
}

// FormatResult formats tool execution results for different contexts
func (t *A2ASubmitTaskTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *A2ASubmitTaskTool) FormatForLLM(result *domain.ToolExecutionResult) string {
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
func (t *A2ASubmitTaskTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return result.Error
	}

	data, ok := result.Data.(A2ASubmitTaskResult)
	if !ok {
		return "A2A task operation completed"
	}

	if data.TaskType == "" {
		return fmt.Sprintf("A2A Task: %s", data.Message)
	}

	taskPreview := data.TaskType
	if len(taskPreview) > 50 {
		taskPreview = taskPreview[:47] + "..."
	}

	if data.WentIdle {
		return fmt.Sprintf("A2A Task: %s (delegated, went idle)", taskPreview)
	}

	return fmt.Sprintf("A2A Task: %s", taskPreview)
}

// FormatForUI formats the result for UI display
func (t *A2ASubmitTaskTool) FormatForUI(result *domain.ToolExecutionResult) string {
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
func (t *A2ASubmitTaskTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *A2ASubmitTaskTool) ShouldAlwaysExpand() bool {
	return false
}

// errorResult creates an error result
func (t *A2ASubmitTaskTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	var taskType string
	if desc, ok := args["task_description"].(string); ok {
		taskType = desc
	}

	return &domain.ToolExecutionResult{
		ToolName:  "A2A_SubmitTask",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: A2ASubmitTaskResult{
			Success:  false,
			Message:  errorMsg,
			TaskType: taskType,
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
func (t *A2ASubmitTaskTool) extractTaskResult(task adk.Task) string {
	if task.Status.Message != nil {
		return extractTextFromParts(task.Status.Message.Parts)
	}
	return ""
}

// isTaskNotFoundError checks if the error indicates a task was not found
func (t *A2ASubmitTaskTool) isTaskNotFoundError(err error) bool {
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
