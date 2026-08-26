package tui

import (
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// ApplicationState represents the overall application state with proper typing
type ApplicationState struct {
	// View Management
	currentView  ViewState
	previousView ViewState

	// Agent Mode
	agentMode agentdomain.AgentMode

	// Chat State
	chatSession *agentdomain.ChatSession

	// Tool Execution State
	toolExecution *agentdomain.ToolExecutionSession

	// Message Queue
	queuedMessages []convdomain.QueuedMessage

	// UI Dimensions
	width  int
	height int

	// UI State
	fileSelectionState  *FileSelectionState
	approvalUIState     *agentdomain.ApprovalUIState
	planApprovalUIState *agentdomain.PlanApprovalUIState
	userQuestionUIState *agentdomain.UserQuestionUIState

	// Todo State
	todos []agentdomain.TodoItem

	// Agent Readiness State
	agentReadiness *AgentReadinessState

	// Message Edit State
	messageEditState *MessageEditState

	// Focus Management (macOS computer-use tools)
	// Stores the application ID of the app that was clicked (for restoring focus before keyboard operations)
	lastFocusedAppID string
	// Stores the coordinates of the last click (for re-clicking before keyboard operations)
	lastClickX int
	lastClickY int

	// Computer Use Pause State
	computerUsePaused bool
	pausedRequestID   string

	// Debugging
	debugMode bool
}

// ViewState represents the current view with proper state management
type ViewState int

const (
	ViewStateModelSelection ViewState = iota
	ViewStateChat
	ViewStateFileSelection
	ViewStateConversationSelection
	ViewStateThemeSelection
	ViewStateA2ATaskManagement
	ViewStatePlanApproval
	ViewStateOpentaskSetup
	ViewStateDiffViewer
	ViewStateExplorer
	ViewStateHelp
	ViewStateToolsList
	ViewStateA2AAgents
)

func (v ViewState) String() string {
	switch v {
	case ViewStateModelSelection:
		return "ModelSelection"
	case ViewStateChat:
		return "Chat"
	case ViewStateFileSelection:
		return "FileSelection"
	case ViewStateConversationSelection:
		return "ConversationSelection"
	case ViewStateThemeSelection:
		return "ThemeSelection"
	case ViewStateA2ATaskManagement:
		return "A2ATaskManagement"
	case ViewStatePlanApproval:
		return "PlanApproval"
	case ViewStateOpentaskSetup:
		return "OpentaskSetup"
	case ViewStateDiffViewer:
		return "DiffViewer"
	case ViewStateExplorer:
		return "Explorer"
	case ViewStateHelp:
		return "Help"
	case ViewStateToolsList:
		return "ToolsList"
	case ViewStateA2AAgents:
		return "A2AAgents"
	default:
		return "Unknown"
	}
}

// FileSelectionState represents the state of file selection UI
type FileSelectionState struct {
	Files         []string `json:"files"`
	SearchQuery   string   `json:"search_query"`
	SelectedIndex int      `json:"selected_index"`
}

// MessageEditState represents the state when editing a message
type MessageEditState struct {
	OriginalMessageIndex int `json:"original_message_index"`
}

// MessageSnapshot represents a snapshot of a message for the history view
type MessageSnapshot struct {
	Index     int             `json:"index"`
	Role      sdk.MessageRole `json:"role"`
	Content   string          `json:"content"`
	Timestamp time.Time       `json:"timestamp"`
}

// NewApplicationState creates a new application state
func NewApplicationState() *ApplicationState {
	return &ApplicationState{
		currentView:        ViewStateModelSelection,
		previousView:       ViewStateModelSelection,
		agentMode:          agentdomain.AgentModeStandard,
		chatSession:        nil,
		toolExecution:      nil,
		queuedMessages:     make([]convdomain.QueuedMessage, 0),
		fileSelectionState: nil,
		debugMode:          false,
	}
}

// GetCurrentView returns the current view state
func (s *ApplicationState) GetCurrentView() ViewState {
	return s.currentView
}

// GetPreviousView returns the previous view state
func (s *ApplicationState) GetPreviousView() ViewState {
	return s.previousView
}

// TransitionToView changes the current view with validation
func (s *ApplicationState) TransitionToView(newView ViewState) error {
	if !s.isValidTransition(s.currentView, newView) {
		return fmt.Errorf("invalid view transition from %s to %s", s.currentView, newView)
	}

	s.previousView = s.currentView
	s.currentView = newView
	return nil
}

// GetAgentMode returns the current agent mode
func (s *ApplicationState) GetAgentMode() agentdomain.AgentMode {
	return s.agentMode
}

// SetAgentMode sets the agent mode
func (s *ApplicationState) SetAgentMode(mode agentdomain.AgentMode) {
	s.agentMode = mode
}

// CycleAgentMode cycles to the next agent mode. The human shift+tab cycle is
// deliberately three-way (Standard -> Plan -> AutoAccept); AgentModeReadOnly is a
// subagent-only capability set by the Agent tool's `type` parameter, not a mode a
// user can toggle their own chat into.
func (s *ApplicationState) CycleAgentMode() agentdomain.AgentMode {
	switch s.agentMode {
	case agentdomain.AgentModeStandard:
		s.agentMode = agentdomain.AgentModePlan
	case agentdomain.AgentModePlan:
		s.agentMode = agentdomain.AgentModeAutoAccept
	case agentdomain.AgentModeAutoAccept:
		s.agentMode = agentdomain.AgentModeStandard
	default:
		s.agentMode = agentdomain.AgentModeStandard
	}
	return s.agentMode
}

// isValidTransition validates if a view transition is allowed
func (s *ApplicationState) isValidTransition(from, to ViewState) bool {
	if from == to {
		return true
	}

	validTransitions := map[ViewState][]ViewState{
		ViewStateModelSelection: {ViewStateChat},
		ViewStateChat: {
			ViewStateModelSelection,
			ViewStateFileSelection,
			ViewStateConversationSelection,
			ViewStateThemeSelection,
			ViewStateA2ATaskManagement,
			ViewStatePlanApproval,
			ViewStateOpentaskSetup,
			ViewStateDiffViewer,
			ViewStateExplorer,
			ViewStateHelp,
			ViewStateToolsList,
			ViewStateA2AAgents,
		},
		ViewStateFileSelection:         {ViewStateChat},
		ViewStateConversationSelection: {ViewStateChat},
		ViewStateThemeSelection:        {ViewStateChat},
		ViewStateA2ATaskManagement:     {ViewStateChat},
		ViewStatePlanApproval:          {ViewStateChat},
		ViewStateOpentaskSetup:         {ViewStateChat},
		ViewStateDiffViewer:            {ViewStateChat},
		ViewStateExplorer:              {ViewStateChat},
		ViewStateHelp:                  {ViewStateChat},
		ViewStateToolsList:             {ViewStateChat},
		ViewStateA2AAgents:             {ViewStateChat},
	}

	allowed, exists := validTransitions[from]
	if !exists {
		return false
	}

	for _, allowedView := range allowed {
		if allowedView == to {
			return true
		}
	}
	return false
}

// SetChatPending creates a minimal chat session to mark the agent as busy
// before the actual chat starts. This prevents race conditions.
func (s *ApplicationState) SetChatPending() {
	s.chatSession = &agentdomain.ChatSession{
		RequestID:    "pending",
		Status:       agentdomain.ChatStatusStarting,
		StartTime:    time.Now(),
		Model:        "",
		EventChannel: nil,
		IsFirstChunk: true,
		HasToolCalls: false,
		LastActivity: time.Now(),
	}
}

// StartChatSession initializes a new chat session
func (s *ApplicationState) StartChatSession(requestID, model string, eventChan <-chan agentdomain.ChatEvent) {
	if s.chatSession != nil {
		s.EndChatSession()
	}

	s.chatSession = &agentdomain.ChatSession{
		RequestID:    requestID,
		Status:       agentdomain.ChatStatusStarting,
		StartTime:    time.Now(),
		Model:        model,
		EventChannel: eventChan,
		IsFirstChunk: true,
		HasToolCalls: false,
		LastActivity: time.Now(),
	}
}

// AddQueuedMessage adds a message to the input queue
func (s *ApplicationState) AddQueuedMessage(message sdk.Message, requestID string) {
	queuedMsg := convdomain.QueuedMessage{
		Message:   message,
		QueuedAt:  time.Now(),
		RequestID: requestID,
	}
	s.queuedMessages = append(s.queuedMessages, queuedMsg)
}

// PopQueuedMessage removes and returns the first message from the queue (FIFO)
func (s *ApplicationState) PopQueuedMessage() *convdomain.QueuedMessage {
	if len(s.queuedMessages) == 0 {
		return nil
	}
	msg := s.queuedMessages[0]
	s.queuedMessages = s.queuedMessages[1:]
	return &msg
}

// ClearQueuedMessages clears all queued messages
func (s *ApplicationState) ClearQueuedMessages() {
	s.queuedMessages = make([]convdomain.QueuedMessage, 0)
}

// GetQueuedMessages returns the current queued messages
func (s *ApplicationState) GetQueuedMessages() []convdomain.QueuedMessage {
	return s.queuedMessages
}

// UpdateChatStatus updates the chat session status
func (s *ApplicationState) UpdateChatStatus(status agentdomain.ChatStatus) error {
	if s.chatSession == nil {
		return fmt.Errorf("no active chat session")
	}

	if !s.isValidChatStatusTransition(s.chatSession.Status, status) {
		return fmt.Errorf("invalid chat status transition from %s to %s",
			s.chatSession.Status, status)
	}

	s.chatSession.Status = status
	s.chatSession.LastActivity = time.Now()
	return nil
}

// isValidChatStatusTransition validates chat status transitions
func (s *ApplicationState) isValidChatStatusTransition(from, to agentdomain.ChatStatus) bool {
	if from == to {
		return true
	}

	validTransitions := map[agentdomain.ChatStatus][]agentdomain.ChatStatus{
		agentdomain.ChatStatusIdle: {agentdomain.ChatStatusStarting},
		agentdomain.ChatStatusStarting: {
			agentdomain.ChatStatusThinking,
			agentdomain.ChatStatusGenerating,
			agentdomain.ChatStatusWaitingTools,
			agentdomain.ChatStatusError,
			agentdomain.ChatStatusCancelled,
		},
		agentdomain.ChatStatusThinking: {
			agentdomain.ChatStatusGenerating,
			agentdomain.ChatStatusReceivingTools,
			agentdomain.ChatStatusWaitingTools,
			agentdomain.ChatStatusCompleted,
			agentdomain.ChatStatusError,
			agentdomain.ChatStatusCancelled,
		},
		agentdomain.ChatStatusGenerating: {
			agentdomain.ChatStatusReceivingTools,
			agentdomain.ChatStatusWaitingTools,
			agentdomain.ChatStatusCompleted,
			agentdomain.ChatStatusError,
			agentdomain.ChatStatusCancelled,
		},
		agentdomain.ChatStatusReceivingTools: {
			agentdomain.ChatStatusWaitingTools,
			agentdomain.ChatStatusCompleted,
			agentdomain.ChatStatusError,
			agentdomain.ChatStatusCancelled,
		},
		agentdomain.ChatStatusWaitingTools: {
			agentdomain.ChatStatusStarting,
			agentdomain.ChatStatusCompleted,
			agentdomain.ChatStatusError,
			agentdomain.ChatStatusCancelled,
		},
		agentdomain.ChatStatusCompleted: {agentdomain.ChatStatusIdle},
		agentdomain.ChatStatusError:     {agentdomain.ChatStatusIdle},
		agentdomain.ChatStatusCancelled: {agentdomain.ChatStatusIdle},
	}

	allowed, exists := validTransitions[from]
	if !exists {
		return false
	}

	for _, allowedStatus := range allowed {
		if allowedStatus == to {
			return true
		}
	}
	return false
}

// EndChatSession cleans up the chat session
func (s *ApplicationState) EndChatSession() {
	s.chatSession = nil
}

// SetRetryStatus updates the retry status on the current chat session
func (s *ApplicationState) SetRetryStatus(status *agentdomain.RetryStatus) {
	if s.chatSession != nil {
		s.chatSession.RetryStatus = status
	}
}

// GetRetryStatus returns a copy of the current retry status, or nil when no
// retry is in progress. Returning a copy keeps callers from sharing the
// mutable pointer with the agent's streaming goroutine.
func (s *ApplicationState) GetRetryStatus() *agentdomain.RetryStatus {
	if s.chatSession == nil || s.chatSession.RetryStatus == nil {
		return nil
	}
	status := *s.chatSession.RetryStatus
	return &status
}

// TouchChatActivity records that the stream produced output: it bumps the
// session's LastActivity and clears any retry status, since receiving a
// chunk means the connection is healthy again.
func (s *ApplicationState) TouchChatActivity() {
	if s.chatSession == nil {
		return
	}
	s.chatSession.LastActivity = time.Now()
	s.chatSession.RetryStatus = nil
}

// GetChatSession returns the current chat session
func (s *ApplicationState) GetChatSession() *agentdomain.ChatSession {
	return s.chatSession
}

// StartToolExecution initializes a new tool execution session
func (s *ApplicationState) StartToolExecution(tools []agentdomain.ToolCall) {
	if len(tools) == 0 {
		return
	}

	s.toolExecution = &agentdomain.ToolExecutionSession{
		CurrentTool:    &tools[0],
		RemainingTools: tools[1:],
		TotalTools:     len(tools),
		CompletedTools: 0,
		Status:         agentdomain.ToolExecutionStatusProcessing,
		StartTime:      time.Now(),
	}
}

// CompleteCurrentTool marks the current tool as completed and moves to next
func (s *ApplicationState) CompleteCurrentTool(result *agentdomain.ToolExecutionResult) error {
	if s.toolExecution == nil || s.toolExecution.CurrentTool == nil {
		return fmt.Errorf("no current tool to complete")
	}

	now := time.Now()
	s.toolExecution.CurrentTool.Status = agentdomain.ToolCallStatusCompleted
	s.toolExecution.CurrentTool.Result = result
	s.toolExecution.CurrentTool.EndTime = &now
	s.toolExecution.CompletedTools++

	return s.moveToNextTool()
}

// FailCurrentTool marks the current tool as failed and moves to next
func (s *ApplicationState) FailCurrentTool(result *agentdomain.ToolExecutionResult) error {
	if s.toolExecution == nil || s.toolExecution.CurrentTool == nil {
		return fmt.Errorf("no current tool to fail")
	}

	now := time.Now()
	s.toolExecution.CurrentTool.Status = agentdomain.ToolCallStatusFailed
	s.toolExecution.CurrentTool.Result = result
	s.toolExecution.CurrentTool.EndTime = &now
	s.toolExecution.CompletedTools++

	return s.moveToNextTool()
}

// moveToNextTool advances to the next tool in the queue
func (s *ApplicationState) moveToNextTool() error {
	if len(s.toolExecution.RemainingTools) == 0 {
		s.toolExecution.Status = agentdomain.ToolExecutionStatusCompleted
		return nil
	}

	s.toolExecution.CurrentTool = &s.toolExecution.RemainingTools[0]
	s.toolExecution.RemainingTools = s.toolExecution.RemainingTools[1:]
	s.toolExecution.Status = agentdomain.ToolExecutionStatusProcessing
	s.toolExecution.CurrentTool.Status = agentdomain.ToolCallStatusPending

	return nil
}

// EndToolExecution cleans up the tool execution session
func (s *ApplicationState) EndToolExecution() {
	s.toolExecution = nil
}

// GetToolExecution returns the current tool execution session
func (s *ApplicationState) GetToolExecution() *agentdomain.ToolExecutionSession {
	return s.toolExecution
}

// SetDimensions updates the UI dimensions
func (s *ApplicationState) SetDimensions(width, height int) {
	s.width = width
	s.height = height
}

// GetDimensions returns the current UI dimensions
func (s *ApplicationState) GetDimensions() (int, int) {
	return s.width, s.height
}

// SetDebugMode enables or disables debug mode
func (s *ApplicationState) SetDebugMode(enabled bool) {
	s.debugMode = enabled
}

// IsDebugMode returns whether debug mode is enabled
func (s *ApplicationState) IsDebugMode() bool {
	return s.debugMode
}

// File Selection State Management

// SetupFileSelection initializes file selection state
func (s *ApplicationState) SetupFileSelection(files []string) {
	s.fileSelectionState = &FileSelectionState{
		Files:         files,
		SearchQuery:   "",
		SelectedIndex: 0,
	}
}

// GetFileSelectionState returns the current file selection state
func (s *ApplicationState) GetFileSelectionState() *FileSelectionState {
	return s.fileSelectionState
}

// UpdateFileSearchQuery updates the file search query
func (s *ApplicationState) UpdateFileSearchQuery(query string) {
	if s.fileSelectionState != nil {
		s.fileSelectionState.SearchQuery = query
		s.fileSelectionState.SelectedIndex = 0 // Reset selection when searching
	}
}

// SetFileSelectedIndex sets the selected file index
func (s *ApplicationState) SetFileSelectedIndex(index int) {
	if s.fileSelectionState != nil {
		s.fileSelectionState.SelectedIndex = index
	}
}

// ClearFileSelectionState clears the file selection state
func (s *ApplicationState) ClearFileSelectionState() {
	s.fileSelectionState = nil
}

// Approval State Management

// SetupApprovalUIState initializes approval UI state with the pending tool call
func (s *ApplicationState) SetupApprovalUIState(toolCall *sdk.ChatCompletionMessageToolCall, responseChan chan agentdomain.ApprovalAction) {
	s.approvalUIState = &agentdomain.ApprovalUIState{
		PendingToolCall: toolCall,
		ResponseChan:    responseChan,
	}
}

// GetApprovalUIState returns the current approval UI state
func (s *ApplicationState) GetApprovalUIState() *agentdomain.ApprovalUIState {
	return s.approvalUIState
}

// ClearApprovalUIState clears the approval UI state
func (s *ApplicationState) ClearApprovalUIState() {
	if s.approvalUIState != nil && s.approvalUIState.ResponseChan != nil {
		close(s.approvalUIState.ResponseChan)
	}
	s.approvalUIState = nil
}

// Plan Approval State Management

// SetupPlanApprovalUIState initializes plan approval UI state
func (s *ApplicationState) SetupPlanApprovalUIState(planContent, planID string, responseChan chan agentdomain.PlanApprovalAction) {
	s.planApprovalUIState = &agentdomain.PlanApprovalUIState{
		SelectedIndex: int(agentdomain.PlanApprovalAccept),
		PlanContent:   planContent,
		PlanID:        planID,
		ResponseChan:  responseChan,
	}
}

// GetPlanApprovalUIState returns the current plan approval UI state
func (s *ApplicationState) GetPlanApprovalUIState() *agentdomain.PlanApprovalUIState {
	return s.planApprovalUIState
}

// SetPlanApprovalSelectedIndex sets the plan approval selection index
func (s *ApplicationState) SetPlanApprovalSelectedIndex(index int) {
	if s.planApprovalUIState != nil {
		s.planApprovalUIState.SelectedIndex = index
	}
}

// ClearPlanApprovalUIState clears the plan approval UI state
func (s *ApplicationState) ClearPlanApprovalUIState() {
	if s.planApprovalUIState != nil && s.planApprovalUIState.ResponseChan != nil {
		close(s.planApprovalUIState.ResponseChan)
	}
	s.planApprovalUIState = nil
}

// User Question State Management

// SetupUserQuestionUIState initializes the AskUserQuestion form state for the
// given questions.
func (s *ApplicationState) SetupUserQuestionUIState(questions []agentdomain.UserQuestion, responseChan chan []agentdomain.UserQuestionAnswer) {
	s.userQuestionUIState = &agentdomain.UserQuestionUIState{
		Questions:    questions,
		ResponseChan: responseChan,
	}
}

// GetUserQuestionUIState returns the current AskUserQuestion form state, or nil.
func (s *ApplicationState) GetUserQuestionUIState() *agentdomain.UserQuestionUIState {
	return s.userQuestionUIState
}

// ClearUserQuestionUIState clears the AskUserQuestion form state and closes the
// response channel (without a prior send this signals cancellation to the
// blocked tool). Nil-safe and idempotent.
func (s *ApplicationState) ClearUserQuestionUIState() {
	if s.userQuestionUIState != nil && s.userQuestionUIState.ResponseChan != nil {
		close(s.userQuestionUIState.ResponseChan)
	}
	s.userQuestionUIState = nil
}

// Todo State Management

// SetTodos sets the todo list
func (s *ApplicationState) SetTodos(todos []agentdomain.TodoItem) {
	s.todos = todos
}

// GetTodos returns the current todo list
func (s *ApplicationState) GetTodos() []agentdomain.TodoItem {
	return s.todos
}

// Message Edit State Management

// SetMessageEditState sets the message edit state
func (s *ApplicationState) SetMessageEditState(state *MessageEditState) {
	s.messageEditState = state
}

// GetMessageEditState returns the current message edit state
func (s *ApplicationState) GetMessageEditState() *MessageEditState {
	return s.messageEditState
}

// ClearMessageEditState clears the message edit state
func (s *ApplicationState) ClearMessageEditState() {
	s.messageEditState = nil
}

// IsEditingMessage returns true if currently editing a message
func (s *ApplicationState) IsEditingMessage() bool {
	return s.messageEditState != nil
}

// Focus Management Methods (macOS computer-use tools)

// SetLastFocusedApp stores the application ID of the last focused application
// This is used to restore focus before keyboard operations
func (s *ApplicationState) SetLastFocusedApp(appID string) {
	s.lastFocusedAppID = appID
}

// GetLastFocusedApp returns the application ID of the last focused application
func (s *ApplicationState) GetLastFocusedApp() string {
	return s.lastFocusedAppID
}

// ClearLastFocusedApp clears the stored focused app
func (s *ApplicationState) ClearLastFocusedApp() {
	s.lastFocusedAppID = ""
}

// SetLastClickCoordinates stores the coordinates of the last click
func (s *ApplicationState) SetLastClickCoordinates(x, y int) {
	s.lastClickX = x
	s.lastClickY = y
}

// GetLastClickCoordinates returns the coordinates of the last click
func (s *ApplicationState) GetLastClickCoordinates() (x, y int) {
	return s.lastClickX, s.lastClickY
}

// ClearLastClickCoordinates clears the stored click coordinates
func (s *ApplicationState) ClearLastClickCoordinates() {
	s.lastClickX = 0
	s.lastClickY = 0
}

// Computer Use Pause Management

// SetComputerUsePaused sets the paused state for computer use
func (s *ApplicationState) SetComputerUsePaused(paused bool, requestID string) {
	s.computerUsePaused = paused
	s.pausedRequestID = requestID
}

// IsComputerUsePaused returns whether computer use is currently paused
func (s *ApplicationState) IsComputerUsePaused() bool {
	return s.computerUsePaused
}

// GetPausedRequestID returns the request ID of the paused execution
func (s *ApplicationState) GetPausedRequestID() string {
	return s.pausedRequestID
}

// ClearComputerUsePauseState clears the pause state
func (s *ApplicationState) ClearComputerUsePauseState() {
	s.computerUsePaused = false
	s.pausedRequestID = ""
}

// AgentReadinessState represents the current state of A2A agents during startup
type AgentReadinessState struct {
	TotalAgents int                     `json:"total_agents"`
	ReadyAgents int                     `json:"ready_agents"`
	Agents      map[string]*AgentStatus `json:"agents"`
	StartTime   time.Time               `json:"start_time"`
}

// AgentStatus represents the status of an individual A2A agent
type AgentStatus struct {
	Name        string                 `json:"name"`
	URL         string                 `json:"url"`
	Image       string                 `json:"image"`
	State       agentdomain.AgentState `json:"state"`
	Message     string                 `json:"message,omitempty"`
	StartTime   time.Time              `json:"start_time"`
	Error       string                 `json:"error,omitempty"`
	LayersDone  int                    `json:"layers_done,omitempty"`
	LayersTotal int                    `json:"layers_total,omitempty"`
}

// Agent Readiness State Management

// InitializeAgentReadiness initializes the agent readiness tracking
func (s *ApplicationState) InitializeAgentReadiness(totalAgents int) {
	s.agentReadiness = &AgentReadinessState{
		TotalAgents: totalAgents,
		ReadyAgents: 0,
		Agents:      make(map[string]*AgentStatus),
		StartTime:   time.Now(),
	}
}

// UpdateAgentStatus updates the status of a specific agent
func (s *ApplicationState) UpdateAgentStatus(name string, state agentdomain.AgentState, message string, url string, image string) {
	if s.agentReadiness == nil {
		return
	}

	agent, exists := s.agentReadiness.Agents[name]
	if !exists {
		agent = &AgentStatus{
			Name:      name,
			URL:       url,
			Image:     image,
			StartTime: time.Now(),
		}
		s.agentReadiness.Agents[name] = agent
	}
	agent.State = state
	agent.Message = message
	s.recountReadyAgents()
}

// recountReadyAgents derives ReadyAgents from the per-agent states so the
// count can never drift when agents flap between Ready and Failed.
func (s *ApplicationState) recountReadyAgents() {
	ready := 0
	for _, agent := range s.agentReadiness.Agents {
		if agent.State == agentdomain.AgentStateReady {
			ready++
		}
	}
	s.agentReadiness.ReadyAgents = ready
}

// UpdateAgentPullProgress updates the image pull layer counts for a specific agent
func (s *ApplicationState) UpdateAgentPullProgress(name string, done, total int) {
	if s.agentReadiness == nil {
		return
	}
	if agent, exists := s.agentReadiness.Agents[name]; exists {
		agent.LayersDone, agent.LayersTotal = done, total
	}
}

// SetAgentError sets an error for a specific agent
func (s *ApplicationState) SetAgentError(name string, err error) {
	if s.agentReadiness == nil {
		return
	}

	agent, exists := s.agentReadiness.Agents[name]
	if !exists {
		agent = &AgentStatus{
			Name:      name,
			StartTime: time.Now(),
		}
		s.agentReadiness.Agents[name] = agent
	}

	agent.State = agentdomain.AgentStateFailed
	agent.Error = err.Error()
	s.recountReadyAgents()
}

// GetAgentReadiness returns the current agent readiness state
func (s *ApplicationState) GetAgentReadiness() *AgentReadinessState {
	return s.agentReadiness
}

// AreAllAgentsReady returns true if all agents are ready
func (s *ApplicationState) AreAllAgentsReady() bool {
	if s.agentReadiness == nil {
		return true // No agents to wait for
	}
	return s.agentReadiness.ReadyAgents >= s.agentReadiness.TotalAgents
}

// ClearAgentReadiness clears the agent readiness state
func (s *ApplicationState) ClearAgentReadiness() {
	s.agentReadiness = nil
}

// RemoveAgent removes an agent from the readiness tracking
func (s *ApplicationState) RemoveAgent(name string) {
	if s.agentReadiness == nil {
		return
	}

	if _, exists := s.agentReadiness.Agents[name]; !exists {
		return
	}

	delete(s.agentReadiness.Agents, name)
	s.agentReadiness.TotalAgents--
	s.recountReadyAgents()
}
