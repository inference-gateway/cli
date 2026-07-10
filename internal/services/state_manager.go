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
	state          *domain.ApplicationState
	mutex          sync.RWMutex
	stallThreshold time.Duration

	// Event multicast for floating window (optional)
	eventBridge domain.EventBridge

	// Debug and audit trail
	debugMode      bool
	stateHistory   []domain.StateSnapshot
	maxHistorySize int
}

// Compile-time assertion that StateManager implements domain.StateManager interface
var _ domain.StateManager = (*StateManager)(nil)

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
		stallThreshold: 5 * time.Second,
		debugMode:      debugMode,
		stateHistory:   make([]domain.StateSnapshot, 0),
		maxHistorySize: 100,
	}
}

// captureStateChange captures a state change for debugging and audit trail
func (sm *StateManager) captureStateChange(_ /* changeType */ StateChangeType, _ domain.StateSnapshot) {
	newState := sm.state.GetStateSnapshot()

	sm.stateHistory = append(sm.stateHistory, newState)
	if len(sm.stateHistory) > sm.maxHistorySize {
		sm.stateHistory = sm.stateHistory[1:]
	}
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
		logger.Error("failed to transition view", "error", err, "newView", newView.String())
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
//
// Promotes to "pending" if there is no existing chat session OR if the existing
// session is in a terminal status (Completed/Error/Cancelled/Idle). Terminal
// sessions are leftovers from a prior chat that hasn't been GC'd yet; treating
// them as "in progress" would make IsAgentBusy() return false here but true
// for a subsequent caller that just won the SetChatPending race, which is
// exactly the window the chat-mode async rollover relies on for queueing.
func (sm *StateManager) SetChatPending() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	existing := sm.state.GetChatSession()
	if existing == nil || isTerminalChatStatus(existing.Status) {
		sm.state.SetChatPending()
	}
}

// SetRetryStatus updates the retry status on the current chat session.
// Called by the HTTP client's OnRetry callback to provide visual feedback
// in the status bar when the client is reconnecting after a failure.
func (sm *StateManager) SetRetryStatus(status *domain.RetryStatus) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.state.SetRetryStatus(status)
}

// GetRetryStatus returns a copy of the current retry status, or nil when the
// connection is healthy. Besides explicit HTTP retries (set by the client's
// OnRetry callback), it synthesizes a zero-attempt status when a streaming
// session has produced no chunks for stallThreshold - the render tick calls
// this on every frame, so a stalled connection surfaces without any timer of
// its own. A synthesized status has Attempt == 0.
func (sm *StateManager) GetRetryStatus() *domain.RetryStatus {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	if status := sm.state.GetRetryStatus(); status != nil {
		return status
	}

	session := sm.state.GetChatSession()
	if session == nil || !chatStatusExpectsChunks(session.Status) {
		return nil
	}
	if sm.stallThreshold > 0 && time.Since(session.LastActivity) > sm.stallThreshold {
		return &domain.RetryStatus{}
	}
	return nil
}

// SetStallThreshold sets how long a streaming session may go without chunks
// before GetRetryStatus reports it as reconnecting. Zero or negative disables
// stall detection.
func (sm *StateManager) SetStallThreshold(threshold time.Duration) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.stallThreshold = threshold
}

// TouchChatActivity records stream output on the current session, clearing
// any retry status and resetting the stall clock.
func (sm *StateManager) TouchChatActivity() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.state.TouchChatActivity()
}

// chatStatusExpectsChunks reports whether the status is one where SSE chunks
// should be flowing; local tool execution and terminal states are excluded so
// a long-running tool doesn't read as a stalled connection.
func chatStatusExpectsChunks(s domain.ChatStatus) bool {
	switch s {
	case domain.ChatStatusStarting, domain.ChatStatusThinking, domain.ChatStatusGenerating, domain.ChatStatusReceivingTools:
		return true
	}
	return false
}

func isTerminalChatStatus(s domain.ChatStatus) bool {
	switch s {
	case domain.ChatStatusIdle, domain.ChatStatusCompleted, domain.ChatStatusError, domain.ChatStatusCancelled:
		return true
	}
	return false
}

// SetEventBridge sets the event bridge for multicasting events to floating window
func (sm *StateManager) SetEventBridge(bridge domain.EventBridge) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.eventBridge = bridge
}

// GetEventBridge returns the event bridge for control event forwarding
func (sm *StateManager) GetEventBridge() domain.EventBridge {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.eventBridge
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
			ID:        tc.ID,
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
}

// BroadcastEvent publishes an event to the EventBridge for floating window
func (sm *StateManager) BroadcastEvent(event domain.ChatEvent) {
	if sm.eventBridge != nil {
		sm.eventBridge.Publish(event)
	}
}

// Plan approval state methods

// SetupPlanApprovalUIState initializes plan approval UI state
func (sm *StateManager) SetupPlanApprovalUIState(planContent, planPath string, responseChan chan domain.PlanApprovalAction) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetupPlanApprovalUIState(planContent, planPath, responseChan)
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

// SetupUserQuestionUIState initializes the AskUserQuestion form state
func (sm *StateManager) SetupUserQuestionUIState(questions []domain.UserQuestion, responseChan chan []domain.UserQuestionAnswer) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetupUserQuestionUIState(questions, responseChan)
}

// GetUserQuestionUIState returns the current AskUserQuestion form state
func (sm *StateManager) GetUserQuestionUIState() *domain.UserQuestionUIState {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.GetUserQuestionUIState()
}

// SetUserQuestionOptionCursor sets the highlighted option row
func (sm *StateManager) SetUserQuestionOptionCursor(idx int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetUserQuestionOptionCursor(idx)
}

// ToggleUserQuestionOption toggles the option at optIdx for the current question
func (sm *StateManager) ToggleUserQuestionOption(optIdx int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.ToggleUserQuestionOption(optIdx)
}

// AdvanceUserQuestion moves to the next question and reports whether done
func (sm *StateManager) AdvanceUserQuestion() bool {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	return sm.state.AdvanceUserQuestion()
}

// SetUserQuestionOtherActive toggles free-text entry on the "Other" row
func (sm *StateManager) SetUserQuestionOtherActive(active bool) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.SetUserQuestionOtherActive(active)
}

// AppendUserQuestionOtherText appends typed text to the current "Other" buffer
func (sm *StateManager) AppendUserQuestionOtherText(text string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.AppendUserQuestionOtherText(text)
}

// BackspaceUserQuestionOtherText removes the last rune from the "Other" buffer
func (sm *StateManager) BackspaceUserQuestionOtherText() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.BackspaceUserQuestionOtherText()
}

// BuildUserQuestionAnswers materializes the answers from the working state
func (sm *StateManager) BuildUserQuestionAnswers() []domain.UserQuestionAnswer {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.BuildUserQuestionAnswers()
}

// ClearUserQuestionUIState clears the AskUserQuestion form state
func (sm *StateManager) ClearUserQuestionUIState() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.ClearUserQuestionUIState()
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

	logger.Warn("attempting to recover from inconsistent state")

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

	logger.Warn("state recovery completed")
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
