package services

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

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
}

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
func NewStateManager(debugMode bool) *StateManager {
	return &StateManager{
		state:          domain.NewApplicationState(),
		listeners:      make([]StateChangeListener, 0),
		debugMode:      debugMode,
		stateHistory:   make([]domain.StateSnapshot, 0),
		maxHistorySize: 100,
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

	if sm.debugMode {
		logger.Debug("State change detected",
			"type", changeType.String(),
			"oldView", oldState.CurrentView,
			"newView", newState.CurrentView,
			"oldChatStatus", sm.getChatStatus(oldState),
			"newChatStatus", sm.getChatStatus(newState),
			"oldToolStatus", sm.getToolStatus(oldState),
			"newToolStatus", sm.getToolStatus(newState),
		)
	}

	sm.stateHistory = append(sm.stateHistory, newState)
	if len(sm.stateHistory) > sm.maxHistorySize {
		sm.stateHistory = sm.stateHistory[1:]
	}

	sm.notifyListeners(oldState, newState)
}

// Helper functions for debugging
func (sm *StateManager) getChatStatus(snapshot domain.StateSnapshot) string {
	if snapshot.ChatSession != nil {
		return snapshot.ChatSession.Status
	}
	return "None"
}

func (sm *StateManager) getToolStatus(snapshot domain.StateSnapshot) string {
	if snapshot.ToolExecution != nil {
		return snapshot.ToolExecution.Status
	}
	return "None"
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
	logger.Debug("View transition successful", "newView", newView.String())
	return nil
}

// StartChatSession starts a new chat session
func (sm *StateManager) StartChatSession(requestID, model string, eventChan <-chan domain.ChatEvent) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	sm.state.StartChatSession(requestID, model, eventChan)
	logger.Debug("Chat session started", "requestID", requestID, "model", model)

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

	logger.Debug("Chat status updated", "status", status.String())
	sm.captureStateChange(StateChangeTypeChatStatus, oldState)
	return nil
}

// EndChatSession ends the current chat session
func (sm *StateManager) EndChatSession() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	sm.state.EndChatSession()
	logger.Debug("Chat session ended")

	sm.captureStateChange(StateChangeTypeChatStatus, oldState)
}

// GetChatSession returns the current chat session (read-only)
func (sm *StateManager) GetChatSession() *domain.ChatSession {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetChatSession()
}

// StartToolExecution starts a new tool execution session
func (sm *StateManager) StartToolExecution(toolCalls []sdk.ChatCompletionMessageToolCall) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	tools := make([]domain.ToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		args := make(map[string]interface{})
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
	logger.Debug("Tool execution session started", "toolCount", len(tools))

	sm.captureStateChange(StateChangeTypeToolExecution, oldState)
	return nil
}

// SetToolApprovalRequired marks the current tool as requiring approval
func (sm *StateManager) SetToolApprovalRequired(required bool) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	if err := sm.state.SetToolApprovalRequired(required); err != nil {
		return err
	}

	logger.Debug("Tool approval requirement set", "required", required)
	sm.captureStateChange(StateChangeTypeToolExecution, oldState)
	return nil
}

// ApproveCurrentTool approves the current tool for execution
func (sm *StateManager) ApproveCurrentTool() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	if err := sm.state.ApproveCurrentTool(); err != nil {
		return err
	}

	logger.Debug("Current tool approved")
	sm.captureStateChange(StateChangeTypeToolExecution, oldState)
	return nil
}

// DenyCurrentTool denies the current tool execution
func (sm *StateManager) DenyCurrentTool() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	if err := sm.state.DenyCurrentTool(); err != nil {
		return err
	}

	logger.Debug("Current tool denied")
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

	logger.Debug("Current tool completed", "success", result.Success)
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

	logger.Debug("Current tool failed", "error", result.Error)
	sm.captureStateChange(StateChangeTypeToolExecution, oldState)
	return nil
}

// EndToolExecution ends the current tool execution session
func (sm *StateManager) EndToolExecution() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	sm.state.EndToolExecution()
	logger.Debug("Tool execution session ended")

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
	logger.Debug("Debug mode changed", "enabled", enabled)
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
	logger.Debug("File selection state setup", "fileCount", len(files))
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
	logger.Debug("File selection state cleared")
}

// SetupApprovalUI initializes approval UI state
func (sm *StateManager) SetupApprovalUI() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetupApprovalUI()
	logger.Debug("Approval UI state setup")
}

// GetApprovalUIState returns the current approval UI state
func (sm *StateManager) GetApprovalUIState() *domain.ApprovalUIState {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetApprovalUIState()
}

// SetApprovalSelectedIndex sets the approval selection index
func (sm *StateManager) SetApprovalSelectedIndex(index int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetApprovalSelectedIndex(index)
}

// ClearApprovalUIState clears the approval UI state
func (sm *StateManager) ClearApprovalUIState() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.ClearApprovalUIState()
	logger.Debug("Approval UI state cleared")
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
