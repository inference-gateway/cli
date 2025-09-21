package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// BackgroundTaskStatus represents the status of a background task
type BackgroundTaskStatus string

const (
	BackgroundTaskStatusPending   BackgroundTaskStatus = "pending"
	BackgroundTaskStatusRunning   BackgroundTaskStatus = "running"
	BackgroundTaskStatusCompleted BackgroundTaskStatus = "completed"
	BackgroundTaskStatusFailed    BackgroundTaskStatus = "failed"
	BackgroundTaskStatusCancelled BackgroundTaskStatus = "cancelled"
)

// BackgroundTask represents a task running in the background
type BackgroundTask struct {
	ID              string               `json:"id"`
	AgentURL        string               `json:"agent_url"`
	Description     string               `json:"description"`
	Status          BackgroundTaskStatus `json:"status"`
	StartTime       time.Time            `json:"start_time"`
	CompletionTime  *time.Time           `json:"completion_time,omitempty"`
	Result          string               `json:"result,omitempty"`
	Error           string               `json:"error,omitempty"`
	TaskID          string               `json:"task_id,omitempty"`
	ContextID       string               `json:"context_id,omitempty"`
	OriginalRequest map[string]any       `json:"original_request"`

	// Internal fields for cancellation
	cancelCtx  context.Context
	cancelFunc context.CancelFunc
}

// BackgroundTaskResult represents the result of a background task
type BackgroundTaskResult struct {
	Task    *BackgroundTask `json:"task"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
}

// BackgroundTaskManager manages background task execution
type BackgroundTaskManager struct {
	config      *config.Config
	mutex       sync.RWMutex
	tasks       map[string]*BackgroundTask
	resultChan  chan BackgroundTaskResult
	eventChan   chan<- domain.UIEvent
	taskTracker domain.TaskTracker
}

// NewBackgroundTaskManager creates a new background task manager
func NewBackgroundTaskManager(cfg *config.Config, taskTracker domain.TaskTracker) *BackgroundTaskManager {
	return &BackgroundTaskManager{
		config:      cfg,
		tasks:       make(map[string]*BackgroundTask),
		resultChan:  make(chan BackgroundTaskResult, 100),
		taskTracker: taskTracker,
	}
}

// SetEventChannel sets the event channel for UI notifications
func (m *BackgroundTaskManager) SetEventChannel(eventChan chan<- domain.UIEvent) {
	m.eventChan = eventChan
}

// SubmitTask submits a task to run in the background
func (m *BackgroundTaskManager) SubmitTask(ctx context.Context, agentURL, description string, args map[string]any) (*domain.BackgroundTask, error) {
	if !m.config.Tools.Task.Enabled {
		return nil, fmt.Errorf("A2A tasks are disabled in configuration")
	}

	taskID := fmt.Sprintf("bg_%d", time.Now().UnixNano())
	taskCtx, cancelFunc := context.WithCancel(ctx)

	task := &BackgroundTask{
		ID:              taskID,
		AgentURL:        agentURL,
		Description:     description,
		Status:          BackgroundTaskStatusPending,
		StartTime:       time.Now(),
		OriginalRequest: args,
		cancelCtx:       taskCtx,
		cancelFunc:      cancelFunc,
	}

	m.mutex.Lock()
	m.tasks[taskID] = task
	m.mutex.Unlock()

	m.emitEvent(domain.BackgroundTaskStartedEvent{
		TaskID:      taskID,
		AgentURL:    agentURL,
		Description: description,
		Timestamp:   time.Now(),
	})

	go m.executeTask(task)

	logger.Info("Background task submitted", "task_id", taskID, "agent_url", agentURL)
	return task, nil
}

// executeTask runs a task in the background
func (m *BackgroundTaskManager) executeTask(task *BackgroundTask) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Background task panicked", "task_id", task.ID, "panic", r)
			m.updateTaskStatus(task.ID, BackgroundTaskStatusFailed, "", fmt.Sprintf("Task panicked: %v", r))
		}
	}()

	m.updateTaskStatus(task.ID, BackgroundTaskStatusRunning, "", "")

	result, err := m.runA2ATask(task)
	if err != nil {
		m.updateTaskStatus(task.ID, BackgroundTaskStatusFailed, "", err.Error())
		m.resultChan <- BackgroundTaskResult{
			Task:    task,
			Success: false,
			Error:   err.Error(),
		}
		return
	}

	m.updateTaskStatus(task.ID, BackgroundTaskStatusCompleted, result, "")
	m.resultChan <- BackgroundTaskResult{
		Task:    task,
		Success: true,
	}
}

// runA2ATask executes the actual A2A task (similar to A2ATaskTool.Execute but background)
func (m *BackgroundTaskManager) runA2ATask(task *BackgroundTask) (string, error) {
	var existingTaskID string
	var contextID string
	if m.taskTracker != nil {
		existingTaskID = m.taskTracker.GetFirstTaskID()
		contextID = m.taskTracker.GetContextID()
	}

	adkTask := adk.Task{
		Kind: "task",
		Metadata: map[string]any{
			"description": task.Description,
		},
		Status: adk.TaskStatus{
			State: adk.TaskStateSubmitted,
		},
	}

	if metadata, exists := task.OriginalRequest["metadata"]; exists {
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			for k, v := range metadataMap {
				adkTask.Metadata[k] = v
			}
		}
	}

	adkClient := client.NewClient(task.AgentURL)
	message := adk.Message{
		Kind: "message",
		Role: "user",
		Parts: []adk.Part{
			map[string]any{
				"kind": "text",
				"text": task.Description,
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

	taskResponse, err := adkClient.SendTask(task.cancelCtx, msgParams)
	if err != nil {
		return "", fmt.Errorf("A2A task submission failed: %w", err)
	}

	var submittedTask adk.Task
	if err := mapToStruct(taskResponse.Result, &submittedTask); err != nil {
		return "", fmt.Errorf("failed to parse task submission response: %w", err)
	}

	if submittedTask.ID == "" {
		return "", fmt.Errorf("task submitted but no task ID received")
	}

	remoteTaskID := submittedTask.ID
	task.TaskID = remoteTaskID

	if submittedTask.ContextID != "" {
		contextID = submittedTask.ContextID
		task.ContextID = submittedTask.ContextID
		if m.taskTracker != nil {
			m.taskTracker.SetContextID(submittedTask.ContextID)
		}
	}

	if m.taskTracker != nil && existingTaskID == "" {
		m.taskTracker.SetFirstTaskID(remoteTaskID)
	}

	maxAttempts := 60
	pollInterval := time.Duration(m.config.A2A.Task.StatusPollSeconds) * time.Second

	for attempt := range maxAttempts {
		select {
		case <-task.cancelCtx.Done():
			return "", fmt.Errorf("task cancelled")
		default:
		}

		if attempt > 0 {
			select {
			case <-task.cancelCtx.Done():
				return "", fmt.Errorf("task cancelled")
			case <-time.After(pollInterval):
			}
		}

		queryParams := adk.TaskQueryParams{ID: remoteTaskID}
		taskStatus, err := adkClient.GetTask(task.cancelCtx, queryParams)
		if err != nil {
			continue
		}

		var currentTask adk.Task
		if err := mapToStruct(taskStatus.Result, &currentTask); err != nil {
			continue
		}

		if currentTask.Status.State == adk.TaskStateCompleted || currentTask.Status.State == adk.TaskStateFailed {
			return extractTaskResult(currentTask), nil
		}

		if currentTask.Status.State == "input-required" {
			return "", fmt.Errorf("task requires input (not supported in background mode)")
		}
	}

	return "", fmt.Errorf("task polling timeout after %d attempts", maxAttempts)
}

// updateTaskStatus updates the status of a background task
func (m *BackgroundTaskManager) updateTaskStatus(taskID string, status BackgroundTaskStatus, result, errorMsg string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	task, exists := m.tasks[taskID]
	if !exists {
		return
	}

	task.Status = status
	if result != "" {
		task.Result = result
	}
	if errorMsg != "" {
		task.Error = errorMsg
	}
	if status == BackgroundTaskStatusCompleted || status == BackgroundTaskStatusFailed || status == BackgroundTaskStatusCancelled {
		now := time.Now()
		task.CompletionTime = &now
	}

	switch status {
	case BackgroundTaskStatusCompleted:
		m.emitEvent(domain.BackgroundTaskCompletedEvent{
			TaskID:    taskID,
			Success:   true,
			Result:    result,
			Timestamp: time.Now(),
		})
	case BackgroundTaskStatusFailed:
		m.emitEvent(domain.BackgroundTaskCompletedEvent{
			TaskID:    taskID,
			Success:   false,
			Error:     errorMsg,
			Timestamp: time.Now(),
		})
	}
}

// GetActiveTasks returns all currently active (non-completed) tasks
func (m *BackgroundTaskManager) GetActiveTasks() []interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var activeTasks []interface{}
	for _, task := range m.tasks {
		if task.Status == BackgroundTaskStatusPending || task.Status == BackgroundTaskStatusRunning {
			activeTasks = append(activeTasks, task)
		}
	}
	return activeTasks
}

// GetAllTasks returns all tasks
func (m *BackgroundTaskManager) GetAllTasks() []interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var allTasks []interface{}
	for _, task := range m.tasks {
		allTasks = append(allTasks, task)
	}
	return allTasks
}

// GetTask returns a specific task by ID
func (m *BackgroundTaskManager) GetTask(taskID string) (interface{}, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	task, exists := m.tasks[taskID]
	return task, exists
}

// CancelTask cancels a background task
func (m *BackgroundTaskManager) CancelTask(taskID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	task, exists := m.tasks[taskID]
	if !exists {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if task.Status != BackgroundTaskStatusPending && task.Status != BackgroundTaskStatusRunning {
		return fmt.Errorf("task cannot be cancelled in current status: %s", task.Status)
	}

	task.cancelFunc()
	task.Status = BackgroundTaskStatusCancelled
	now := time.Now()
	task.CompletionTime = &now

	logger.Info("Background task cancelled", "task_id", taskID)
	return nil
}

// GetActiveTaskCount returns the number of active tasks
func (m *BackgroundTaskManager) GetActiveTaskCount() int {
	return len(m.GetActiveTasks())
}

// GetResultChannel returns the result channel for consuming task completions
func (m *BackgroundTaskManager) GetResultChannel() <-chan BackgroundTaskResult {
	return m.resultChan
}

// CleanupOldTasks removes completed tasks older than specified duration
func (m *BackgroundTaskManager) CleanupOldTasks(maxAge time.Duration) int {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	cutoff := time.Now().Add(-maxAge)
	cleaned := 0

	for taskID, task := range m.tasks {
		if task.CompletionTime != nil && task.CompletionTime.Before(cutoff) {
			delete(m.tasks, taskID)
			cleaned++
		}
	}

	if cleaned > 0 {
		logger.Info("Cleaned up old background tasks", "count", cleaned)
	}

	return cleaned
}

// emitEvent sends an event to the UI if event channel is available
func (m *BackgroundTaskManager) emitEvent(event domain.UIEvent) {
	if m.eventChan != nil {
		select {
		case m.eventChan <- event:
		default:
			logger.Warn("Failed to send background task event: channel full")
		}
	}
}

// Helper functions

// mapToStruct converts a map[string]any to a struct using JSON marshaling (copied from a2a_task.go)
func mapToStruct(data any, target any) error {
	if data == nil || target == nil {
		return fmt.Errorf("data or target is nil")
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, target)
}

// extractTaskResult extracts the result text from a completed or failed task (copied from a2a_task.go)
func extractTaskResult(task adk.Task) string {
	if task.Status.Message != nil {
		return extractTextFromParts(task.Status.Message.Parts)
	}
	return ""
}

// extractTextFromParts extracts text content from message parts (copied from a2a_task.go)
func extractTextFromParts(parts []adk.Part) string {
	var result string
	for _, part := range parts {
		if partMap, ok := part.(map[string]any); ok {
			if text, exists := partMap["text"]; exists {
				if textStr, ok := text.(string); ok {
					result += textStr
				}
			}
		}
	}
	return result
}
