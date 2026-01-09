package services

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// StateManager provides centralized state management with proper synchronization
type StateManager struct {
	state *domain.ApplicationState
	mutex sync.RWMutex

	// State change listeners
	listeners []StateChangeListener

	// Event multicast for floating window (optional)
	eventBridge domain.EventBridge

	// Debug and audit trail
	debugMode      bool
	stateHistory   []domain.StateSnapshot
	maxHistorySize int
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
func (sm *StateManager) captureStateChange(_ /* changeType */ StateChangeType, oldState domain.StateSnapshot) {
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

// GetPreviousView returns the previous view state
func (sm *StateManager) GetPreviousView() domain.ViewState {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetPreviousView()
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

// GetAgentMode returns the current agent mode
func (sm *StateManager) GetAgentMode() domain.AgentMode {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetAgentMode()
}

// SetAgentMode sets the agent mode
func (sm *StateManager) SetAgentMode(mode domain.AgentMode) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()
	sm.state.SetAgentMode(mode)
	sm.captureStateChange(StateChangeTypeViewTransition, oldState)
}

// CycleAgentMode cycles to the next agent mode
func (sm *StateManager) CycleAgentMode() domain.AgentMode {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()
	newMode := sm.state.CycleAgentMode()
	sm.captureStateChange(StateChangeTypeViewTransition, oldState)

	return newMode
}

// SetChatPending marks the agent as busy before the chat actually starts.
// This prevents race conditions where messages might not be queued
// between the time we decide to start a chat and when StartChatSession is called.
func (sm *StateManager) SetChatPending() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if sm.state.GetChatSession() == nil {
		sm.state.SetChatPending()
	}
}

// SetEventBridge sets the event bridge for multicasting events to floating window
func (sm *StateManager) SetEventBridge(bridge domain.EventBridge) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.eventBridge = bridge
}

// StartChatSession starts a new chat session
func (sm *StateManager) StartChatSession(requestID, model string, eventChan <-chan domain.ChatEvent) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	oldState := sm.state.GetStateSnapshot()

	if sm.eventBridge != nil {
		eventChan = sm.eventBridge.Tap(eventChan)
	}

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

	toolExecution := sm.state.GetToolExecution()
	if toolExecution != nil {
		return true
	}

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

// Approval state methods

// SetupApprovalUIState initializes approval UI state
func (sm *StateManager) SetupApprovalUIState(toolCall *sdk.ChatCompletionMessageToolCall, responseChan chan domain.ApprovalAction) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetupApprovalUIState(toolCall, responseChan)
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

	if sm.eventBridge != nil {
		requestID := ""
		if chatSession := sm.state.GetChatSession(); chatSession != nil {
			requestID = chatSession.RequestID
		}
		sm.eventBridge.Publish(domain.ToolApprovalClearedEvent{
			RequestID: requestID,
			Timestamp: time.Now(),
		})
	}
}

// BroadcastEvent publishes an event to the EventBridge for floating window
func (sm *StateManager) BroadcastEvent(event domain.ChatEvent) {
	if sm.eventBridge != nil {
		sm.eventBridge.Publish(event)
	}
}

// Plan approval state methods

// SetupPlanApprovalUIState initializes plan approval UI state
func (sm *StateManager) SetupPlanApprovalUIState(planContent string, responseChan chan domain.PlanApprovalAction) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetupPlanApprovalUIState(planContent, responseChan)
}

// GetPlanApprovalUIState returns the current plan approval UI state
func (sm *StateManager) GetPlanApprovalUIState() *domain.PlanApprovalUIState {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.GetPlanApprovalUIState()
}

// SetPlanApprovalSelectedIndex sets the plan approval selection index
func (sm *StateManager) SetPlanApprovalSelectedIndex(index int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetPlanApprovalSelectedIndex(index)
}

// ClearPlanApprovalUIState clears the plan approval UI state
func (sm *StateManager) ClearPlanApprovalUIState() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.ClearPlanApprovalUIState()
}

// Todo management methods

// SetTodos sets the todo list
func (sm *StateManager) SetTodos(todos []domain.TodoItem) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetTodos(todos)
}

// GetTodos returns the current todo list
func (sm *StateManager) GetTodos() []domain.TodoItem {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.GetTodos()
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

// Message edit state methods

// SetMessageEditState sets the message edit state
func (sm *StateManager) SetMessageEditState(state *domain.MessageEditState) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetMessageEditState(state)
}

// GetMessageEditState returns the current message edit state
func (sm *StateManager) GetMessageEditState() *domain.MessageEditState {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.GetMessageEditState()
}

// ClearMessageEditState clears the message edit state
func (sm *StateManager) ClearMessageEditState() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.ClearMessageEditState()
}

// IsEditingMessage returns true if currently editing a message
func (sm *StateManager) IsEditingMessage() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.IsEditingMessage()
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

// Agent Readiness Management

// InitializeAgentReadiness initializes the agent readiness tracking
func (sm *StateManager) InitializeAgentReadiness(totalAgents int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.InitializeAgentReadiness(totalAgents)
}

// UpdateAgentStatus updates the status of a specific agent
func (sm *StateManager) UpdateAgentStatus(name string, state domain.AgentState, message string, url string, image string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.UpdateAgentStatus(name, state, message, url, image)
}

// SetAgentError sets an error for a specific agent
func (sm *StateManager) SetAgentError(name string, err error) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetAgentError(name, err)
}

// GetAgentReadiness returns the current agent readiness state
func (sm *StateManager) GetAgentReadiness() *domain.AgentReadinessState {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.GetAgentReadiness()
}

// AreAllAgentsReady returns true if all agents are ready
func (sm *StateManager) AreAllAgentsReady() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.AreAllAgentsReady()
}

// ClearAgentReadiness clears the agent readiness state
func (sm *StateManager) ClearAgentReadiness() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.ClearAgentReadiness()
}

// RemoveAgent removes an agent from the readiness tracking
func (sm *StateManager) RemoveAgent(name string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.RemoveAgent(name)
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

// Focus management methods (macOS computer-use tools)

// SetLastFocusedApp stores the bundle ID of the last focused application
func (sm *StateManager) SetLastFocusedApp(appID string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.state.SetLastFocusedApp(appID)
}

// GetLastFocusedApp returns the bundle ID of the last focused application
func (sm *StateManager) GetLastFocusedApp() string {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetLastFocusedApp()
}

// ClearLastFocusedApp clears the stored focused app
func (sm *StateManager) ClearLastFocusedApp() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.state.ClearLastFocusedApp()
}

// SetLastClickCoordinates stores the coordinates of the last click
func (sm *StateManager) SetLastClickCoordinates(x, y int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.state.SetLastClickCoordinates(x, y)
}

// GetLastClickCoordinates returns the coordinates of the last click
func (sm *StateManager) GetLastClickCoordinates() (x, y int) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetLastClickCoordinates()
}

// ClearLastClickCoordinates clears the stored click coordinates
func (sm *StateManager) ClearLastClickCoordinates() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.state.ClearLastClickCoordinates()
}

// Computer Use Pause State Management

// SetComputerUsePaused sets the paused state for computer use
func (sm *StateManager) SetComputerUsePaused(paused bool, requestID string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.state.SetComputerUsePaused(paused, requestID)
}

// IsComputerUsePaused returns whether computer use is currently paused
func (sm *StateManager) IsComputerUsePaused() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.IsComputerUsePaused()
}

// GetPausedRequestID returns the request ID of the paused execution
func (sm *StateManager) GetPausedRequestID() string {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.GetPausedRequestID()
}

// ClearComputerUsePauseState clears the pause state
func (sm *StateManager) ClearComputerUsePauseState() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.state.ClearComputerUsePauseState()
}
