package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// A2ATaskTool handles A2A task submission and management
type A2ATaskTool struct {
	config    *config.Config
	eventChan chan<- domain.UIEvent
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
func NewA2ATaskTool(cfg *config.Config) *A2ATaskTool {
	return &A2ATaskTool{
		config: cfg,
	}
}

// SetEventChannel sets the event channel for streaming events to the UI
func (t *A2ATaskTool) SetEventChannel(eventChan chan<- domain.UIEvent) {
	t.eventChan = eventChan
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
func (t *A2ATaskTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) { // nolint:gocyclo,cyclop,funlen
	startTime := time.Now()

	// TODO - need to improve this

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

	agentURL, ok := args["agent_url"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_url parameter is required and must be a string")
	}

	taskDescription, ok := args["task_description"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_description parameter is required and must be a string")
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

	adkClient := client.NewClient(agentURL)
	msgParams := adk.MessageSendParams{
		Message: adk.Message{
			Kind: "message",
			Role: "user",
			Parts: []adk.Part{
				map[string]any{
					"kind": "text",
					"text": taskDescription,
				},
			},
		},
		Configuration: &adk.MessageSendConfiguration{
			Blocking:            &[]bool{true}[0],
			AcceptedOutputModes: []string{"text"},
		},
	}

	if t.eventChan != nil {
		t.eventChan <- domain.SetStatusEvent{
			Message:    fmt.Sprintf("Submitting task to %s...", agentURL),
			Spinner:    true,
			StatusType: domain.StatusProcessing,
		}
	}

	var finalResult string
	var eventCount int

	timeout := time.After(30 * time.Second)

	adkEventChan, err := adkClient.SendTaskStreaming(ctx, msgParams)
	if err != nil {
		if t.eventChan != nil {
			t.eventChan <- domain.ShowErrorEvent{
				Error:  fmt.Sprintf("A2A task streaming failed: %v", err),
				Sticky: false,
			}
		}
		return t.errorResult(args, startTime, fmt.Sprintf("A2A task streaming failed: %v", err))
	}

streamLoop:
	for {
		select {
		case <-ctx.Done():
			if t.eventChan != nil {
				t.eventChan <- domain.ShowErrorEvent{
					Error:  "A2A task cancelled",
					Sticky: false,
				}
			}
			return t.errorResult(args, startTime, "Task cancelled")
		case <-timeout:
			if t.eventChan != nil {
				t.eventChan <- domain.ShowErrorEvent{
					Error:  "A2A task timed out after 30 seconds",
					Sticky: false,
				}
			}
			return t.errorResult(args, startTime, "Task timed out")
		case event, ok := <-adkEventChan:
			if !ok {
				break streamLoop
			}

			eventCount++

			if event.Result == nil {
				continue
			}

			eventData, ok := event.Result.(map[string]any)
			if !ok {
				continue
			}

			eventKind, exists := eventData["kind"]
			if !exists {
				continue
			}

			eventKindStr, ok := eventKind.(string)
			if !ok {
				continue
			}

			switch eventKindStr {
			case "message":
				t.handleMessageEvent(eventData, &finalResult)
			case "task-status-update":
				t.handleTaskStatusEvent(eventData)
			case "artifact-update":
				t.handleArtifactEvent(eventData)
			case "input-required":
				t.handleInputRequiredEvent(eventData)
			default:
				t.handleDefaultEvent(eventData, eventKindStr, &finalResult)
			}
		}
	}

	adkTask.Status.State = adk.TaskStateCompleted
	if finalResult != "" {
		adkTask.Metadata["result"] = finalResult
	}

	if t.eventChan != nil {
		t.eventChan <- domain.SetStatusEvent{
			Message:    fmt.Sprintf("A2A task completed at %s", agentURL),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	logger.Debug("A2A task completed", "task_id", adkTask.ID, "agent_url", agentURL, "event_count", eventCount)

	return &domain.ToolExecutionResult{
		ToolName:  "Task",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: A2ATaskResult{
			AgentName: agentURL,
			Task:      &adkTask,
			Success:   true,
			Message:   fmt.Sprintf("A2A task submitted to %s", agentURL),
			Duration:  time.Since(startTime),
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

func (t *A2ATaskTool) handleMessageEvent(eventData map[string]any, finalResult *string) {
	eventBytes, err := json.Marshal(eventData)
	if err != nil {
		return
	}

	var message adk.Message
	if json.Unmarshal(eventBytes, &message) != nil {
		return
	}

	for _, part := range message.Parts {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}

		partKind, exists := partMap["kind"]
		if !exists || partKind != "text" {
			continue
		}

		text, exists := partMap["text"]
		if !exists {
			continue
		}

		textStr, ok := text.(string)
		if !ok {
			continue
		}

		*finalResult += textStr
	}
}

func (t *A2ATaskTool) handleTaskStatusEvent(eventData map[string]any) {
	if t.eventChan == nil {
		return
	}

	message := "Processing task..."

	eventBytes, err := json.Marshal(eventData)
	if err != nil {
		return
	}

	var statusEvent adk.TaskStatusUpdateEvent
	if json.Unmarshal(eventBytes, &statusEvent) != nil {
		return
	}

	status := string(statusEvent.Status.State)
	if statusEvent.Status.Message == nil {
		return
	}

	for _, part := range statusEvent.Status.Message.Parts {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}

		partKind, exists := partMap["kind"]
		if !exists || partKind != "text" {
			continue
		}

		text, exists := partMap["text"]
		if !exists {
			continue
		}

		textStr, ok := text.(string)
		if !ok {
			continue
		}

		message = textStr
		break
	}

	var statusType domain.StatusType
	var showSpinner bool
	switch status {
	case "working", "submitted":
		statusType = domain.StatusProcessing
		showSpinner = true
	case "completed":
		statusType = domain.StatusDefault
		showSpinner = false
	case "failed", "canceled":
		statusType = domain.StatusDefault
		showSpinner = false
	default:
		statusType = domain.StatusProcessing
		showSpinner = true
	}

	t.eventChan <- domain.SetStatusEvent{
		Message:    fmt.Sprintf("A2A: %s", message),
		Spinner:    showSpinner,
		StatusType: statusType,
	}
}

func (t *A2ATaskTool) handleArtifactEvent(eventData map[string]any) {
	if t.eventChan == nil {
		return
	}

	artifactMessage := "Updating artifact..."

	eventBytes, err := json.Marshal(eventData)
	if err != nil {
		return
	}

	var artifactEvent adk.TaskArtifactUpdateEvent
	if json.Unmarshal(eventBytes, &artifactEvent) != nil {
		return
	}

	if artifactEvent.Artifact.Name != nil {
		artifactMessage = fmt.Sprintf("Updating '%s'...", *artifactEvent.Artifact.Name)
	}

	t.eventChan <- domain.SetStatusEvent{
		Message:    fmt.Sprintf("A2A: %s", artifactMessage),
		Spinner:    true,
		StatusType: domain.StatusProcessing,
	}
}

func (t *A2ATaskTool) handleInputRequiredEvent(eventData map[string]any) {
	if t.eventChan == nil {
		return
	}

	message := "Input required"

	eventBytes, err := json.Marshal(eventData)
	if err != nil {
		return
	}

	var inputEvent adk.Message
	if json.Unmarshal(eventBytes, &inputEvent) != nil {
		return
	}

	for _, part := range inputEvent.Parts {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}

		partKind, exists := partMap["kind"]
		if !exists || partKind != "text" {
			continue
		}

		text, exists := partMap["text"]
		if !exists {
			continue
		}

		textStr, ok := text.(string)
		if !ok {
			continue
		}

		message = textStr
		break
	}

	t.eventChan <- domain.SetStatusEvent{
		Message:    fmt.Sprintf("A2A: %s", message),
		Spinner:    false,
		StatusType: domain.StatusDefault,
	}
}

func (t *A2ATaskTool) handleDefaultEvent(eventData map[string]any, eventKind string, finalResult *string) {
	if content, exists := eventData["content"]; exists {
		if contentStr, ok := content.(string); ok {
			*finalResult += contentStr
		}
	}

	if t.eventChan != nil {
		t.eventChan <- domain.SetStatusEvent{
			Message:    fmt.Sprintf("A2A: Processing %s event", eventKind),
			Spinner:    true,
			StatusType: domain.StatusProcessing,
		}
	}
}
