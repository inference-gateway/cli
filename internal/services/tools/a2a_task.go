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
	AgentName      string        `json:"agent_name"`
	Task           *adk.Task     `json:"task"`
	Success        bool          `json:"success"`
	Message        string        `json:"message"`
	Duration       time.Duration `json:"duration"`
	TaskResult     string        `json:"task_result"`
	TaskType       string        `json:"task_type"`
	TimedOut       bool          `json:"timed_out"`
	WentIdle       bool          `json:"went_idle"`
	IdleTimeout    time.Duration `json:"idle_timeout,omitempty"`
	PollAttempts   int           `json:"poll_attempts,omitempty"`
	PollingDetails string        `json:"polling_details,omitempty"`
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
	description := "Submit work to an A2A agent server and delegate it to run in the background. This tool returns IMMEDIATELY after submission - you don't need to wait or poll manually. The system will automatically monitor the task and emit an event when it completes. After submission, you can continue with other work or ask the user if there's anything else to do while waiting. Use this for ANY interaction where you need an agent to respond with answers or complete work. The Query tool is ONLY for retrieving agent metadata/capabilities (agent card) or checking status of previously submitted tasks."
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
//
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
			Message:    fmt.Sprintf("Task delegated to %s and monitoring in background", agentURL),
			Duration:   time.Since(startTime),
			TaskType:   taskDescription,
			TaskResult: fmt.Sprintf("Task %s delegated successfully. You will be notified automatically when it completes - no need to poll manually.", taskID),
		},
	}, nil
}

// pollTaskInBackground polls for task completion in a background goroutine
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

	adkClient := t.getOrCreateClient(agentURL)

	strategy := t.config.A2A.Task.PollingStrategy
	if strategy == "immediate_idle" {
		t.handleImmediateIdle(agentURL, taskID, state)
		return
	}

	currentInterval, deadline := t.initializePollingStrategy(agentURL, taskID, strategy)
	state.CurrentInterval = currentInterval
	state.NextPollTime = time.Now().Add(currentInterval)

	pollAttempt := 0
	var pollingDetails strings.Builder

	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if state.ErrorChan != nil {
				state.ErrorChan <- fmt.Errorf("task cancelled")
			}
			return

		case <-ticker.C:
			pollAttempt++
			pollingDetails.WriteString(fmt.Sprintf("Poll #%d: interval=%v, elapsed=%v\n",
				pollAttempt, currentInterval, time.Since(state.StartedAt)))

			shouldStop, stopResult := t.checkIdleConditions(agentURL, taskID, strategy, currentInterval, deadline, pollAttempt, state, pollingDetails.String())
			if shouldStop {
				if stopResult != nil && state.ResultChan != nil {
					state.ResultChan <- stopResult
				}
				return
			}

			state.LastPollAt = time.Now()

			currentTask, err := t.queryTask(ctx, adkClient, taskID)
			if err != nil {
				currentInterval = t.handleQueryError(agentURL, taskID, strategy, currentInterval, state, ticker, err)
				continue
			}

			if currentTask == nil {
				currentInterval = t.handleQueryError(agentURL, taskID, strategy, currentInterval, state, ticker, fmt.Errorf("failed to parse task"))
				continue
			}

			t.publishStatusUpdate(state, taskID, agentURL, *currentTask)

			shouldReturn, taskResult := t.handleTaskState(agentURL, taskID, pollAttempt, state, *currentTask, pollingDetails.String())
			if shouldReturn {
				if taskResult != nil && state.ResultChan != nil {
					state.ResultChan <- taskResult
				}
				return
			}

			currentInterval = t.applyExponentialBackoff(agentURL, taskID, strategy, currentInterval, pollAttempt, state, ticker)
		}
	}
}

func (t *A2ASubmitTaskTool) getOrCreateClient(agentURL string) client.A2AClient {
	if t.client != nil {
		return t.client
	}
	return client.NewClient(agentURL)
}

func (t *A2ASubmitTaskTool) handleImmediateIdle(agentURL, taskID string, state *domain.TaskPollingState) {
	idleTimeout := time.Duration(t.config.A2A.Task.IdleTimeoutSec) * time.Second
	result := &domain.ToolExecutionResult{
		ToolName: "A2A_SubmitTask",
		Success:  true,
		Duration: time.Since(state.StartedAt),
		Data: A2ASubmitTaskResult{
			AgentName:   agentURL,
			Success:     true,
			Message:     "Task delegated and went idle immediately",
			WentIdle:    true,
			IdleTimeout: idleTimeout,
		},
	}
	if state.ResultChan != nil {
		state.ResultChan <- result
	}
}

func (t *A2ASubmitTaskTool) initializePollingStrategy(agentURL, taskID, strategy string) (time.Duration, time.Time) {
	var currentInterval time.Duration

	if strategy == "exponential" {
		currentInterval = time.Duration(t.config.A2A.Task.InitialPollIntervalSec) * time.Second
	} else {
		currentInterval = time.Duration(t.config.A2A.Task.StatusPollSeconds) * time.Second
	}

	idleTimeout := time.Duration(t.config.A2A.Task.IdleTimeoutSec) * time.Second
	deadline := time.Now().Add(idleTimeout)

	return currentInterval, deadline
}

func (t *A2ASubmitTaskTool) checkIdleConditions(agentURL, taskID, strategy string, currentInterval time.Duration, deadline time.Time, pollAttempt int, state *domain.TaskPollingState, pollingDetails string) (bool, *domain.ToolExecutionResult) {
	idleTimeout := time.Duration(t.config.A2A.Task.IdleTimeoutSec) * time.Second

	if strategy == "exponential" {
		maxInterval := time.Duration(t.config.A2A.Task.MaxPollIntervalSec) * time.Second
		if currentInterval >= maxInterval {
			result := &domain.ToolExecutionResult{
				ToolName: "A2A_SubmitTask",
				Success:  true,
				Duration: time.Since(state.StartedAt),
				Data: A2ASubmitTaskResult{
					AgentName:      agentURL,
					Success:        true,
					Message:        fmt.Sprintf("Task went idle after reaching max poll interval of %v", maxInterval),
					WentIdle:       true,
					IdleTimeout:    idleTimeout,
					PollAttempts:   pollAttempt,
					PollingDetails: pollingDetails,
				},
			}
			return true, result
		}
	} else if time.Now().After(deadline) {
		result := &domain.ToolExecutionResult{
			ToolName: "A2A_SubmitTask",
			Success:  true,
			Duration: time.Since(state.StartedAt),
			Data: A2ASubmitTaskResult{
				AgentName:      agentURL,
				Success:        true,
				Message:        fmt.Sprintf("Task went idle after %v", idleTimeout),
				WentIdle:       true,
				IdleTimeout:    idleTimeout,
				PollAttempts:   pollAttempt,
				PollingDetails: pollingDetails,
			},
		}
		return true, result
	}

	return false, nil
}

func (t *A2ASubmitTaskTool) queryTask(ctx context.Context, adkClient client.A2AClient, taskID string) (*adk.Task, error) {
	queryParams := adk.TaskQueryParams{ID: taskID}
	taskStatus, err := adkClient.GetTask(ctx, queryParams)
	if err != nil {
		return nil, err
	}

	var currentTask adk.Task
	if err := mapToStruct(taskStatus.Result, &currentTask); err != nil {
		return nil, err
	}

	return &currentTask, nil
}

func (t *A2ASubmitTaskTool) handleQueryError(agentURL, taskID, strategy string, currentInterval time.Duration, state *domain.TaskPollingState, ticker *time.Ticker, err error) time.Duration {
	if strategy != "exponential" {
		return currentInterval
	}

	newInterval := time.Duration(float64(currentInterval) * t.config.A2A.Task.BackoffMultiplier)
	maxInterval := time.Duration(t.config.A2A.Task.MaxPollIntervalSec) * time.Second
	if newInterval > maxInterval {
		newInterval = maxInterval
	}

	state.CurrentInterval = newInterval
	state.NextPollTime = time.Now().Add(newInterval)
	ticker.Reset(newInterval)

	return newInterval
}

func (t *A2ASubmitTaskTool) publishStatusUpdate(state *domain.TaskPollingState, taskID, agentURL string, currentTask adk.Task) {
	if state.StatusChan == nil {
		return
	}

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

func (t *A2ASubmitTaskTool) handleTaskState(agentURL, taskID string, pollAttempt int, state *domain.TaskPollingState, currentTask adk.Task, pollingDetails string) (bool, *domain.ToolExecutionResult) {
	switch currentTask.Status.State {
	case adk.TaskStateCompleted:
		finalResult := t.extractTaskResult(currentTask)
		result := &domain.ToolExecutionResult{
			ToolName: "A2A_SubmitTask",
			Success:  true,
			Duration: time.Since(state.StartedAt),
			Data: A2ASubmitTaskResult{
				AgentName:      agentURL,
				Success:        true,
				Message:        fmt.Sprintf("Task %s", currentTask.Status.State),
				Duration:       time.Since(state.StartedAt),
				TaskResult:     finalResult,
				PollAttempts:   pollAttempt,
				PollingDetails: pollingDetails,
			},
		}
		return true, result

	case adk.TaskStateFailed:
		finalResult := t.extractTaskResult(currentTask)
		result := &domain.ToolExecutionResult{
			ToolName: "A2A_SubmitTask",
			Success:  false,
			Duration: time.Since(state.StartedAt),
			Data: A2ASubmitTaskResult{
				AgentName:      agentURL,
				Success:        false,
				Message:        fmt.Sprintf("Task %s", currentTask.Status.State),
				Duration:       time.Since(state.StartedAt),
				TaskResult:     finalResult,
				PollAttempts:   pollAttempt,
				PollingDetails: pollingDetails,
			},
		}
		return true, result

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
				AgentName:      agentURL,
				Success:        true,
				Message:        fmt.Sprintf("Task requires input: %s", inputMessage),
				TaskResult:     inputMessage,
				PollAttempts:   pollAttempt,
				PollingDetails: pollingDetails,
			},
		}
		return true, result
	}

	return false, nil
}

func (t *A2ASubmitTaskTool) applyExponentialBackoff(agentURL, taskID, strategy string, currentInterval time.Duration, pollAttempt int, state *domain.TaskPollingState, ticker *time.Ticker) time.Duration {
	if strategy != "exponential" {
		return currentInterval
	}

	newInterval := time.Duration(float64(currentInterval) * t.config.A2A.Task.BackoffMultiplier)
	maxInterval := time.Duration(t.config.A2A.Task.MaxPollIntervalSec) * time.Second
	if newInterval > maxInterval {
		newInterval = maxInterval
	}

	state.CurrentInterval = newInterval
	state.NextPollTime = time.Now().Add(newInterval)
	ticker.Reset(newInterval)

	return newInterval
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
