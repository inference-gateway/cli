package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// ADKClientFactory creates ADK clients for specific agent URLs
type ADKClientFactory func(agentURL string) client.A2AClient

// StateManager provides centralized state management with proper synchronization
type StateManager struct {
	state *domain.ApplicationState
	mutex sync.RWMutex

	// State change listeners
	listeners []StateChangeListener

	// Debug and audit trail
	debugMode      bool
	stateHistory   []domain.StateSnapshot
	maxHistorySize int

	// ADK client factory for creating clients per agent URL
	createADKClient ADKClientFactory
}

// Compile-time assertion that StateManager implements domain.StateManager interface
var _ domain.StateManager = (*StateManager)(nil)

// StateChangeListener interface for components that need to react to state changes
type StateChangeListener interface {
	OnStateChanged(oldState, newState domain.StateSnapshot)
}

// StateChangeEvent represents a state change event
type StateChangeEvent struct {
	Type      StateChangeType
	OldState  domain.StateSnapshot
	NewState  domain.StateSnapshot
	Timestamp time.Time
}

// StateChangeType represents the type of state change
type StateChangeType int

const (
	StateChangeTypeViewTransition StateChangeType = iota
	StateChangeTypeChatStatus
	StateChangeTypeToolExecution
	StateChangeTypeDimensions
)

func (s StateChangeType) String() string {
	switch s {
	case StateChangeTypeViewTransition:
		return "ViewTransition"
	case StateChangeTypeChatStatus:
		return "ChatStatus"
	case StateChangeTypeToolExecution:
		return "ToolExecution"
	case StateChangeTypeDimensions:
		return "Dimensions"
	default:
		return "Unknown"
	}
}

// NewStateManager creates a new state manager
func NewStateManager(debugMode bool, createADKClient ADKClientFactory) *StateManager {
	return &StateManager{
		state:           domain.NewApplicationState(),
		listeners:       make([]StateChangeListener, 0),
		debugMode:       debugMode,
		stateHistory:    make([]domain.StateSnapshot, 0),
		maxHistorySize:  100,
		createADKClient: createADKClient,
	}
}

// AddListener adds a state change listener
func (sm *StateManager) AddListener(listener StateChangeListener) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.listeners = append(sm.listeners, listener)
}

// RemoveListener removes a state change listener
func (sm *StateManager) RemoveListener(listener StateChangeListener) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for i, l := range sm.listeners {
		if l == listener {
			sm.listeners = append(sm.listeners[:i], sm.listeners[i+1:]...)
			break
		}
	}
}

// notifyListeners notifies all listeners of a state change
func (sm *StateManager) notifyListeners(oldState, newState domain.StateSnapshot) {
	for _, listener := range sm.listeners {
		go listener.OnStateChanged(oldState, newState)
	}
}

// captureStateChange captures a state change for debugging and audit trail
func (sm *StateManager) captureStateChange(changeType StateChangeType, oldState domain.StateSnapshot) {
	newState := sm.state.GetStateSnapshot()

	sm.stateHistory = append(sm.stateHistory, newState)
	if len(sm.stateHistory) > sm.maxHistorySize {
		sm.stateHistory = sm.stateHistory[1:]
	}

	sm.notifyListeners(oldState, newState)
}

// GetCurrentView returns the current view state
func (sm *StateManager) GetCurrentView() domain.ViewState {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetCurrentView()
}

// TransitionToView transitions to a new view with validation and logging
func (sm *StateManager) TransitionToView(newView domain.ViewState) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	if err := sm.state.TransitionToView(newView); err != nil {
		logger.Error("Failed to transition view", "error", err, "newView", newView.String())
		return err
	}

	sm.captureStateChange(StateChangeTypeViewTransition, oldState)
	return nil
}

// StartChatSession starts a new chat session
func (sm *StateManager) StartChatSession(requestID, model string, eventChan <-chan domain.ChatEvent) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	sm.state.StartChatSession(requestID, model, eventChan)

	sm.captureStateChange(StateChangeTypeChatStatus, oldState)
	return nil
}

// UpdateChatStatus updates the chat session status with validation
func (sm *StateManager) UpdateChatStatus(status domain.ChatStatus) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	currentSession := sm.state.GetChatSession()
	if currentSession != nil && currentSession.Status == status {
		return nil
	}

	if err := sm.state.UpdateChatStatus(status); err != nil {
		return err
	}

	sm.captureStateChange(StateChangeTypeChatStatus, oldState)
	return nil
}

// EndChatSession ends the current chat session
func (sm *StateManager) EndChatSession() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	sm.state.EndChatSession()

	sm.captureStateChange(StateChangeTypeChatStatus, oldState)
}

// GetChatSession returns the current chat session (read-only)
func (sm *StateManager) GetChatSession() *domain.ChatSession {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetChatSession()
}

// IsAgentBusy returns true if the agent is currently processing a request
func (sm *StateManager) IsAgentBusy() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	chatSession := sm.state.GetChatSession()
	if chatSession == nil {
		return false
	}

	switch chatSession.Status {
	case domain.ChatStatusIdle, domain.ChatStatusCompleted, domain.ChatStatusError, domain.ChatStatusCancelled:
		return false
	default:
		return true
	}
}

// StartToolExecution starts a new tool execution session
func (sm *StateManager) StartToolExecution(toolCalls []sdk.ChatCompletionMessageToolCall) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	tools := make([]domain.ToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		args := make(map[string]any)
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}

		tools[i] = domain.ToolCall{
			ID:        tc.Id,
			Name:      tc.Function.Name,
			Arguments: args,
			Status:    domain.ToolCallStatusPending,
			StartTime: time.Now(),
		}
	}

	sm.state.StartToolExecution(tools)

	sm.captureStateChange(StateChangeTypeToolExecution, oldState)
	return nil
}

// CompleteCurrentTool marks the current tool as completed
func (sm *StateManager) CompleteCurrentTool(result *domain.ToolExecutionResult) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	if err := sm.state.CompleteCurrentTool(result); err != nil {
		return err
	}

	sm.captureStateChange(StateChangeTypeToolExecution, oldState)
	return nil
}

// FailCurrentTool marks the current tool as failed
func (sm *StateManager) FailCurrentTool(result *domain.ToolExecutionResult) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	if err := sm.state.FailCurrentTool(result); err != nil {
		return err
	}

	sm.captureStateChange(StateChangeTypeToolExecution, oldState)
	return nil
}

// EndToolExecution ends the current tool execution session
func (sm *StateManager) EndToolExecution() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	sm.state.EndToolExecution()

	sm.captureStateChange(StateChangeTypeToolExecution, oldState)
}

// GetToolExecution returns the current tool execution session (read-only)
func (sm *StateManager) GetToolExecution() *domain.ToolExecutionSession {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetToolExecution()
}

// SetDimensions updates the UI dimensions
func (sm *StateManager) SetDimensions(width, height int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	sm.state.SetDimensions(width, height)

	sm.captureStateChange(StateChangeTypeDimensions, oldState)
}

// GetDimensions returns the current UI dimensions
func (sm *StateManager) GetDimensions() (int, int) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetDimensions()
}

// GetStateSnapshot returns the current state snapshot
func (sm *StateManager) GetStateSnapshot() domain.StateSnapshot {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetStateSnapshot()
}

// GetStateHistory returns the state change history
func (sm *StateManager) GetStateHistory() []domain.StateSnapshot {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	// Return a copy to prevent external modifications
	history := make([]domain.StateSnapshot, len(sm.stateHistory))
	copy(history, sm.stateHistory)
	return history
}

// ExportStateHistory exports the state history as JSON for debugging
func (sm *StateManager) ExportStateHistory() ([]byte, error) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return json.MarshalIndent(sm.stateHistory, "", "  ")
}

// SetDebugMode enables or disables debug mode
func (sm *StateManager) SetDebugMode(enabled bool) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.debugMode = enabled
	sm.state.SetDebugMode(enabled)
}

// IsDebugMode returns whether debug mode is enabled
func (sm *StateManager) IsDebugMode() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.debugMode
}

// ValidateState performs comprehensive state validation
func (sm *StateManager) ValidateState() []error {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	var errors []error

	if chatSession := sm.state.GetChatSession(); chatSession != nil {
		if chatSession.RequestID == "" {
			errors = append(errors, fmt.Errorf("chat session has empty request ID"))
		}
		if chatSession.Model == "" {
			errors = append(errors, fmt.Errorf("chat session has empty model"))
		}
		if chatSession.LastActivity.IsZero() {
			errors = append(errors, fmt.Errorf("chat session has zero last activity time"))
		}
	}

	if toolExecution := sm.state.GetToolExecution(); toolExecution != nil {
		if toolExecution.CurrentTool == nil && len(toolExecution.RemainingTools) > 0 {
			errors = append(errors, fmt.Errorf("tool execution has remaining tools but no current tool"))
		}
		if toolExecution.CompletedTools > toolExecution.TotalTools {
			errors = append(errors, fmt.Errorf("completed tools count exceeds total tools"))
		}
		if toolExecution.CompletedTools < 0 {
			errors = append(errors, fmt.Errorf("completed tools count is negative"))
		}
	}

	return errors
}

// SetupFileSelection initializes file selection state
func (sm *StateManager) SetupFileSelection(files []string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetupFileSelection(files)
}

// GetFileSelectionState returns the current file selection state
func (sm *StateManager) GetFileSelectionState() *domain.FileSelectionState {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetFileSelectionState()
}

// UpdateFileSearchQuery updates the file search query
func (sm *StateManager) UpdateFileSearchQuery(query string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.UpdateFileSearchQuery(query)
}

// SetFileSelectedIndex sets the selected file index
func (sm *StateManager) SetFileSelectedIndex(index int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetFileSelectedIndex(index)
}

// ClearFileSelectionState clears the file selection state
func (sm *StateManager) ClearFileSelectionState() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.ClearFileSelectionState()
}

// AddQueuedMessage adds a message to the input queue
func (sm *StateManager) AddQueuedMessage(message sdk.Message, requestID string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()
	sm.state.AddQueuedMessage(message, requestID)
	sm.captureStateChange(StateChangeTypeChatStatus, oldState)
}

// PopQueuedMessage removes and returns the first message from the queue (FIFO order)
func (sm *StateManager) PopQueuedMessage() *domain.QueuedMessage {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()
	msg := sm.state.PopQueuedMessage()
	sm.captureStateChange(StateChangeTypeChatStatus, oldState)
	return msg
}

// ClearQueuedMessages clears all queued messages
func (sm *StateManager) ClearQueuedMessages() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()
	sm.state.ClearQueuedMessages()
	sm.captureStateChange(StateChangeTypeChatStatus, oldState)
}

// GetQueuedMessages returns the current queued messages
func (sm *StateManager) GetQueuedMessages() []domain.QueuedMessage {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetQueuedMessages()
}

// GetBackgroundTasks returns the current background polling tasks sorted by start time
func (sm *StateManager) GetBackgroundTasks(toolService domain.ToolService) []domain.TaskPollingState {
	if toolService == nil {
		return []domain.TaskPollingState{}
	}

	taskTracker := toolService.GetTaskTracker()
	if taskTracker == nil {
		return []domain.TaskPollingState{}
	}

	pollingTasks := taskTracker.GetAllPollingTasks()
	tasks := make([]domain.TaskPollingState, 0, len(pollingTasks))

	for _, taskID := range pollingTasks {
		if state := taskTracker.GetPollingState(taskID); state != nil {
			tasks = append(tasks, *state)
		}
	}

	return tasks
}

// CancelBackgroundTask cancels a background task by task ID
func (sm *StateManager) CancelBackgroundTask(taskID string, toolService domain.ToolService) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	logger.Info("Canceling background task", "task_id", taskID)

	backgroundTasks := sm.getBackgroundTasksInternal(toolService)

	var targetTask *domain.TaskPollingState
	for i := range backgroundTasks {
		if backgroundTasks[i].TaskID == taskID {
			targetTask = &backgroundTasks[i]
			break
		}
	}

	if targetTask == nil {
		return fmt.Errorf("task %s not found in background tasks", taskID)
	}

	logger.Debug("Found task to cancel", "task_id", taskID, "agent_url", targetTask.AgentURL)

	if err := sm.sendCancelToAgent(targetTask); err != nil {
		logger.Error("Failed to send cancel to agent", "task_id", taskID, "error", err)
	} else {
		logger.Info("Successfully sent cancel request to agent", "task_id", taskID)
	}

	if targetTask.CancelFunc != nil {
		logger.Debug("Triggering local context cancellation", "task_id", taskID)
		targetTask.CancelFunc()
	}

	taskTracker := toolService.GetTaskTracker()
	if taskTracker != nil {
		logger.Debug("Stopping polling and removing from tracker", "task_id", taskID)
		taskTracker.StopPolling(taskID)
		taskTracker.RemoveTask(taskID)
	}

	logger.Info("Task cancelled successfully", "task_id", taskID)
	return nil
}

// sendCancelToAgent sends a cancel request to the agent server
func (sm *StateManager) sendCancelToAgent(task *domain.TaskPollingState) error {
	adkClient := sm.createADKClient(task.AgentURL)

	logger.Debug("Sending CancelTask request to agent", "task_id", task.TaskID, "agent_url", task.AgentURL)
	_, err := adkClient.CancelTask(context.Background(), adk.TaskIdParams{
		ID: task.TaskID,
	})

	if err != nil {
		logger.Error("ADK CancelTask returned error", "task_id", task.TaskID, "error", err)
		return err
	}

	logger.Debug("ADK CancelTask succeeded", "task_id", task.TaskID)
	return nil
}

// getBackgroundTasksInternal is an internal version that doesn't lock (caller must lock)
func (sm *StateManager) getBackgroundTasksInternal(toolService domain.ToolService) []domain.TaskPollingState {
	if toolService == nil {
		return []domain.TaskPollingState{}
	}

	taskTracker := toolService.GetTaskTracker()
	if taskTracker == nil {
		return []domain.TaskPollingState{}
	}

	pollingTasks := taskTracker.GetAllPollingTasks()
	tasks := make([]domain.TaskPollingState, 0, len(pollingTasks))

	for _, taskID := range pollingTasks {
		if state := taskTracker.GetPollingState(taskID); state != nil {
			tasks = append(tasks, *state)
		}
	}

	return tasks
}

// RecoverFromInconsistentState attempts to recover from an inconsistent state
func (sm *StateManager) RecoverFromInconsistentState() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	logger.Warn("Attempting to recover from inconsistent state")

	sm.state.EndChatSession()
	sm.state.EndToolExecution()

	currentView := sm.state.GetCurrentView()
	if currentView != domain.ViewStateChat && currentView != domain.ViewStateModelSelection {
		if err := sm.state.TransitionToView(domain.ViewStateChat); err != nil {
			if currentView != domain.ViewStateModelSelection {
				_ = sm.state.TransitionToView(domain.ViewStateModelSelection)
			}
		}
	}

	logger.Warn("State recovery completed")
	sm.captureStateChange(StateChangeTypeViewTransition, oldState)

	return nil
}

// GetHealthStatus returns the health status of the state manager
func (sm *StateManager) GetHealthStatus() HealthStatus {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	status := HealthStatus{
		Healthy:          true,
		ValidationErrors: sm.ValidateState(),
		StateHistorySize: len(sm.stateHistory),
		LastStateChange:  time.Time{},
		MemoryUsageKB:    0,
	}

	if len(sm.stateHistory) > 0 {
		status.LastStateChange = sm.stateHistory[len(sm.stateHistory)-1].Timestamp
	}

	if len(status.ValidationErrors) > 0 {
		status.Healthy = false
	}

	return status
}

// HealthStatus represents the health status of the state manager
type HealthStatus struct {
	Healthy          bool      `json:"healthy"`
	ValidationErrors []error   `json:"validation_errors"`
	StateHistorySize int       `json:"state_history_size"`
	LastStateChange  time.Time `json:"last_state_change"`
	MemoryUsageKB    int       `json:"memory_usage_kb"`
}
