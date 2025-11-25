package domain

import (
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// ApplicationState represents the overall application state with proper typing
type ApplicationState struct {
	// View Management
	currentView  ViewState
	previousView ViewState

	// Agent Mode
	agentMode AgentMode

	// Chat State
	chatSession *ChatSession

	// Tool Execution State
	toolExecution *ToolExecutionSession

	// Message Queue
	queuedMessages []QueuedMessage

	// UI Dimensions
	width  int
	height int

	// UI State
	fileSelectionState  *FileSelectionState
	approvalUIState     *ApprovalUIState
	planApprovalUIState *PlanApprovalUIState

	// Todo State
	todos []TodoItem

	// Agent Readiness State
	agentReadiness *AgentReadinessState

	// Debugging
	debugMode bool
}

// ViewState represents the current view with proper state management
type ViewState int

const (
	ViewStateModelSelection ViewState = iota
	ViewStateChat
	ViewStateFileSelection
	ViewStateTextSelection
	ViewStateConversationSelection
	ViewStateThemeSelection
	ViewStateA2AServers
	ViewStateA2ATaskManagement
	ViewStatePlanApproval
)

// AgentMode represents the operational mode of the agent
type AgentMode int

const (
	// AgentModeStandard is the default mode with all configured tools and approval checks
	AgentModeStandard AgentMode = iota
	// AgentModePlan is a read-only mode for planning without execution
	AgentModePlan
	// AgentModeAutoAccept bypasses all approval checks (YOLO mode)
	AgentModeAutoAccept
)

func (v ViewState) String() string {
	switch v {
	case ViewStateModelSelection:
		return "ModelSelection"
	case ViewStateChat:
		return "Chat"
	case ViewStateFileSelection:
		return "FileSelection"
	case ViewStateTextSelection:
		return "TextSelection"
	case ViewStateConversationSelection:
		return "ConversationSelection"
	case ViewStateThemeSelection:
		return "ThemeSelection"
	case ViewStateA2AServers:
		return "A2AServers"
	case ViewStateA2ATaskManagement:
		return "A2ATaskManagement"
	case ViewStatePlanApproval:
		return "PlanApproval"
	default:
		return "Unknown"
	}
}

func (m AgentMode) String() string {
	switch m {
	case AgentModeStandard:
		return "Standard"
	case AgentModePlan:
		return "Plan"
	case AgentModeAutoAccept:
		return "AutoAccept"
	default:
		return "Unknown"
	}
}

// DisplayName returns a user-friendly display name for the mode
func (m AgentMode) DisplayName() string {
	switch m {
	case AgentModeStandard:
		return "Standard"
	case AgentModePlan:
		return "Plan Mode"
	case AgentModeAutoAccept:
		return "Auto-Accept"
	default:
		return "Unknown"
	}
}

// QueuedMessage represents a message in the input queue
type QueuedMessage struct {
	Message   sdk.Message
	QueuedAt  time.Time
	RequestID string
}

// ChatSession represents an active chat session state
type ChatSession struct {
	RequestID    string
	Status       ChatStatus
	StartTime    time.Time
	Model        string
	EventChannel <-chan ChatEvent
	IsFirstChunk bool
	HasToolCalls bool
	LastActivity time.Time
}

// ChatStatus represents the current chat operation status
type ChatStatus int

const (
	ChatStatusIdle ChatStatus = iota
	ChatStatusStarting
	ChatStatusThinking
	ChatStatusGenerating
	ChatStatusReceivingTools
	ChatStatusWaitingTools
	ChatStatusCompleted
	ChatStatusError
	ChatStatusCancelled
)

func (c ChatStatus) String() string {
	switch c {
	case ChatStatusIdle:
		return "Idle"
	case ChatStatusStarting:
		return "Starting"
	case ChatStatusThinking:
		return "Thinking"
	case ChatStatusGenerating:
		return "Generating"
	case ChatStatusReceivingTools:
		return "ReceivingTools"
	case ChatStatusWaitingTools:
		return "WaitingTools"
	case ChatStatusCompleted:
		return "Completed"
	case ChatStatusError:
		return "Error"
	case ChatStatusCancelled:
		return "Cancelled"
	default:
		return "Unknown"
	}
}

// ToolExecutionSession represents an active tool execution session
type ToolExecutionSession struct {
	CurrentTool    *ToolCall
	RemainingTools []ToolCall
	TotalTools     int
	CompletedTools int
	Status         ToolExecutionStatus
	StartTime      time.Time
}

// ToolCall represents a tool call with proper typing
type ToolCall struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Arguments map[string]any       `json:"arguments"`
	Status    ToolCallStatus       `json:"status"`
	Result    *ToolExecutionResult `json:"result,omitempty"`
	StartTime time.Time            `json:"start_time"`
	EndTime   *time.Time           `json:"end_time,omitempty"`
}

// ToolCallStatus represents the status of an individual tool call
type ToolCallStatus int

const (
	ToolCallStatusPending ToolCallStatus = iota
	ToolCallStatusWaitingApproval
	ToolCallStatusExecuting
	ToolCallStatusCompleted
	ToolCallStatusFailed
	ToolCallStatusCancelled
	ToolCallStatusDenied
)

func (t ToolCallStatus) String() string {
	switch t {
	case ToolCallStatusPending:
		return "Pending"
	case ToolCallStatusWaitingApproval:
		return "WaitingApproval"
	case ToolCallStatusExecuting:
		return "Executing"
	case ToolCallStatusCompleted:
		return "Completed"
	case ToolCallStatusFailed:
		return "Failed"
	case ToolCallStatusCancelled:
		return "Cancelled"
	case ToolCallStatusDenied:
		return "Denied"
	default:
		return "Unknown"
	}
}

// ToolExecutionStatus represents the overall tool execution session status
type ToolExecutionStatus int

const (
	ToolExecutionStatusIdle ToolExecutionStatus = iota
	ToolExecutionStatusProcessing
	ToolExecutionStatusExecuting
	ToolExecutionStatusCompleted
	ToolExecutionStatusFailed
)

func (t ToolExecutionStatus) String() string {
	switch t {
	case ToolExecutionStatusIdle:
		return "Idle"
	case ToolExecutionStatusProcessing:
		return "Processing"
	case ToolExecutionStatusExecuting:
		return "Executing"
	case ToolExecutionStatusCompleted:
		return "Completed"
	case ToolExecutionStatusFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// ApprovalAction represents the user's choice for tool approval
type ApprovalAction int

const (
	ApprovalApprove ApprovalAction = iota
	ApprovalReject
	ApprovalAutoAccept
)

func (a ApprovalAction) String() string {
	switch a {
	case ApprovalApprove:
		return "Approve"
	case ApprovalReject:
		return "Reject"
	case ApprovalAutoAccept:
		return "Auto-Accept"
	default:
		return "Unknown"
	}
}

// PlanApprovalAction represents the user's choice for plan approval
type PlanApprovalAction int

const (
	PlanApprovalAccept PlanApprovalAction = iota
	PlanApprovalReject
	PlanApprovalAcceptAndAutoApprove
)

func (a PlanApprovalAction) String() string {
	switch a {
	case PlanApprovalAccept:
		return "Accept"
	case PlanApprovalReject:
		return "Reject"
	case PlanApprovalAcceptAndAutoApprove:
		return "Accept & Auto-Approve"
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

// ApprovalUIState represents the state of approval UI
type ApprovalUIState struct {
	SelectedIndex   int                                `json:"selected_index"`
	PendingToolCall *sdk.ChatCompletionMessageToolCall `json:"pending_tool_call"`
	ResponseChan    chan ApprovalAction                `json:"-"`
}

// PlanApprovalUIState represents the state of plan approval UI
type PlanApprovalUIState struct {
	SelectedIndex int                     `json:"selected_index"`
	PlanContent   string                  `json:"plan_content"`
	ResponseChan  chan PlanApprovalAction `json:"-"`
}

// NewApplicationState creates a new application state
func NewApplicationState() *ApplicationState {
	return &ApplicationState{
		currentView:        ViewStateModelSelection,
		previousView:       ViewStateModelSelection,
		agentMode:          AgentModeStandard,
		chatSession:        nil,
		toolExecution:      nil,
		queuedMessages:     make([]QueuedMessage, 0),
		fileSelectionState: nil,
		debugMode:          false,
	}
}

// GetCurrentView returns the current view state
func (s *ApplicationState) GetCurrentView() ViewState {
	return s.currentView
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
func (s *ApplicationState) GetAgentMode() AgentMode {
	return s.agentMode
}

// SetAgentMode sets the agent mode
func (s *ApplicationState) SetAgentMode(mode AgentMode) {
	s.agentMode = mode
}

// CycleAgentMode cycles to the next agent mode
func (s *ApplicationState) CycleAgentMode() AgentMode {
	switch s.agentMode {
	case AgentModeStandard:
		s.agentMode = AgentModePlan
	case AgentModePlan:
		s.agentMode = AgentModeAutoAccept
	case AgentModeAutoAccept:
		s.agentMode = AgentModeStandard
	default:
		s.agentMode = AgentModeStandard
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
			ViewStateTextSelection,
			ViewStateConversationSelection,
			ViewStateThemeSelection,
			ViewStateA2AServers,
			ViewStateA2ATaskManagement,
			ViewStatePlanApproval,
		},
		ViewStateFileSelection:         {ViewStateChat},
		ViewStateTextSelection:         {ViewStateChat},
		ViewStateConversationSelection: {ViewStateChat},
		ViewStateThemeSelection:        {ViewStateChat},
		ViewStateA2AServers:            {ViewStateChat},
		ViewStateA2ATaskManagement:     {ViewStateChat},
		ViewStatePlanApproval:          {ViewStateChat},
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
	s.chatSession = &ChatSession{
		RequestID:    "pending",
		Status:       ChatStatusStarting,
		StartTime:    time.Now(),
		Model:        "",
		EventChannel: nil,
		IsFirstChunk: true,
		HasToolCalls: false,
		LastActivity: time.Now(),
	}
}

// StartChatSession initializes a new chat session
func (s *ApplicationState) StartChatSession(requestID, model string, eventChan <-chan ChatEvent) {
	s.chatSession = &ChatSession{
		RequestID:    requestID,
		Status:       ChatStatusStarting,
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
	queuedMsg := QueuedMessage{
		Message:   message,
		QueuedAt:  time.Now(),
		RequestID: requestID,
	}
	s.queuedMessages = append(s.queuedMessages, queuedMsg)
}

// PopQueuedMessage removes and returns the first message from the queue (FIFO)
func (s *ApplicationState) PopQueuedMessage() *QueuedMessage {
	if len(s.queuedMessages) == 0 {
		return nil
	}
	msg := s.queuedMessages[0]
	s.queuedMessages = s.queuedMessages[1:]
	return &msg
}

// ClearQueuedMessages clears all queued messages
func (s *ApplicationState) ClearQueuedMessages() {
	s.queuedMessages = make([]QueuedMessage, 0)
}

// GetQueuedMessages returns the current queued messages
func (s *ApplicationState) GetQueuedMessages() []QueuedMessage {
	return s.queuedMessages
}

// UpdateChatStatus updates the chat session status
func (s *ApplicationState) UpdateChatStatus(status ChatStatus) error {
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
func (s *ApplicationState) isValidChatStatusTransition(from, to ChatStatus) bool {
	if from == to {
		return true
	}

	validTransitions := map[ChatStatus][]ChatStatus{
		ChatStatusIdle: {ChatStatusStarting},
		ChatStatusStarting: {
			ChatStatusThinking,
			ChatStatusGenerating,
			ChatStatusError,
			ChatStatusCancelled,
		},
		ChatStatusThinking: {
			ChatStatusGenerating,
			ChatStatusReceivingTools,
			ChatStatusCompleted,
			ChatStatusError,
			ChatStatusCancelled,
		},
		ChatStatusGenerating: {
			ChatStatusReceivingTools,
			ChatStatusCompleted,
			ChatStatusError,
			ChatStatusCancelled,
		},
		ChatStatusReceivingTools: {
			ChatStatusWaitingTools,
			ChatStatusCompleted,
			ChatStatusError,
			ChatStatusCancelled,
		},
		ChatStatusWaitingTools: {
			ChatStatusStarting,
			ChatStatusCompleted,
			ChatStatusError,
			ChatStatusCancelled,
		},
		ChatStatusCompleted: {ChatStatusIdle},
		ChatStatusError:     {ChatStatusIdle},
		ChatStatusCancelled: {ChatStatusIdle},
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

// GetChatSession returns the current chat session
func (s *ApplicationState) GetChatSession() *ChatSession {
	return s.chatSession
}

// StartToolExecution initializes a new tool execution session
func (s *ApplicationState) StartToolExecution(tools []ToolCall) {
	if len(tools) == 0 {
		return
	}

	s.toolExecution = &ToolExecutionSession{
		CurrentTool:    &tools[0],
		RemainingTools: tools[1:],
		TotalTools:     len(tools),
		CompletedTools: 0,
		Status:         ToolExecutionStatusProcessing,
		StartTime:      time.Now(),
	}
}

// CompleteCurrentTool marks the current tool as completed and moves to next
func (s *ApplicationState) CompleteCurrentTool(result *ToolExecutionResult) error {
	if s.toolExecution == nil || s.toolExecution.CurrentTool == nil {
		return fmt.Errorf("no current tool to complete")
	}

	now := time.Now()
	s.toolExecution.CurrentTool.Status = ToolCallStatusCompleted
	s.toolExecution.CurrentTool.Result = result
	s.toolExecution.CurrentTool.EndTime = &now
	s.toolExecution.CompletedTools++

	return s.moveToNextTool()
}

// FailCurrentTool marks the current tool as failed and moves to next
func (s *ApplicationState) FailCurrentTool(result *ToolExecutionResult) error {
	if s.toolExecution == nil || s.toolExecution.CurrentTool == nil {
		return fmt.Errorf("no current tool to fail")
	}

	now := time.Now()
	s.toolExecution.CurrentTool.Status = ToolCallStatusFailed
	s.toolExecution.CurrentTool.Result = result
	s.toolExecution.CurrentTool.EndTime = &now
	s.toolExecution.CompletedTools++

	return s.moveToNextTool()
}

// moveToNextTool advances to the next tool in the queue
func (s *ApplicationState) moveToNextTool() error {
	if len(s.toolExecution.RemainingTools) == 0 {
		s.toolExecution.Status = ToolExecutionStatusCompleted
		return nil
	}

	s.toolExecution.CurrentTool = &s.toolExecution.RemainingTools[0]
	s.toolExecution.RemainingTools = s.toolExecution.RemainingTools[1:]
	s.toolExecution.Status = ToolExecutionStatusProcessing
	s.toolExecution.CurrentTool.Status = ToolCallStatusPending

	return nil
}

// EndToolExecution cleans up the tool execution session
func (s *ApplicationState) EndToolExecution() {
	s.toolExecution = nil
}

// GetToolExecution returns the current tool execution session
func (s *ApplicationState) GetToolExecution() *ToolExecutionSession {
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

// GetStateSnapshot returns a complete snapshot of the current state
func (s *ApplicationState) GetStateSnapshot() StateSnapshot {
	snapshot := StateSnapshot{
		CurrentView:  s.currentView.String(),
		PreviousView: s.previousView.String(),
		Width:        s.width,
		Height:       s.height,
		DebugMode:    s.debugMode,
		Timestamp:    time.Now(),
	}

	if s.chatSession != nil {
		snapshot.ChatSession = &ChatSessionSnapshot{
			RequestID:    s.chatSession.RequestID,
			Status:       s.chatSession.Status.String(),
			Model:        s.chatSession.Model,
			StartTime:    s.chatSession.StartTime,
			IsFirstChunk: s.chatSession.IsFirstChunk,
			HasToolCalls: s.chatSession.HasToolCalls,
			LastActivity: s.chatSession.LastActivity,
		}
	}

	if s.toolExecution != nil {
		snapshot.ToolExecution = &ToolExecutionSnapshot{
			Status:         s.toolExecution.Status.String(),
			TotalTools:     s.toolExecution.TotalTools,
			CompletedTools: s.toolExecution.CompletedTools,
			StartTime:      s.toolExecution.StartTime,
		}

		if s.toolExecution.CurrentTool != nil {
			snapshot.ToolExecution.CurrentTool = &ToolCallSnapshot{
				ID:        s.toolExecution.CurrentTool.ID,
				Name:      s.toolExecution.CurrentTool.Name,
				Status:    s.toolExecution.CurrentTool.Status.String(),
				StartTime: s.toolExecution.CurrentTool.StartTime,
			}
		}
	}

	return snapshot
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
func (s *ApplicationState) SetupApprovalUIState(toolCall *sdk.ChatCompletionMessageToolCall, responseChan chan ApprovalAction) {
	s.approvalUIState = &ApprovalUIState{
		SelectedIndex:   int(ApprovalApprove), // Default to approve
		PendingToolCall: toolCall,
		ResponseChan:    responseChan,
	}
}

// GetApprovalUIState returns the current approval UI state
func (s *ApplicationState) GetApprovalUIState() *ApprovalUIState {
	return s.approvalUIState
}

// SetApprovalSelectedIndex sets the approval selection index
func (s *ApplicationState) SetApprovalSelectedIndex(index int) {
	if s.approvalUIState != nil {
		s.approvalUIState.SelectedIndex = index
	}
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
func (s *ApplicationState) SetupPlanApprovalUIState(planContent string, responseChan chan PlanApprovalAction) {
	s.planApprovalUIState = &PlanApprovalUIState{
		SelectedIndex: int(PlanApprovalAccept), // Default to accept
		PlanContent:   planContent,
		ResponseChan:  responseChan,
	}
}

// GetPlanApprovalUIState returns the current plan approval UI state
func (s *ApplicationState) GetPlanApprovalUIState() *PlanApprovalUIState {
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

// Todo State Management

// SetTodos sets the todo list
func (s *ApplicationState) SetTodos(todos []TodoItem) {
	s.todos = todos
}

// GetTodos returns the current todo list
func (s *ApplicationState) GetTodos() []TodoItem {
	return s.todos
}

// StateSnapshot represents a point-in-time snapshot of application state
type StateSnapshot struct {
	CurrentView   string                 `json:"current_view"`
	PreviousView  string                 `json:"previous_view"`
	Width         int                    `json:"width"`
	Height        int                    `json:"height"`
	DebugMode     bool                   `json:"debug_mode"`
	Timestamp     time.Time              `json:"timestamp"`
	ChatSession   *ChatSessionSnapshot   `json:"chat_session,omitempty"`
	ToolExecution *ToolExecutionSnapshot `json:"tool_execution,omitempty"`
}

// ChatSessionSnapshot represents a snapshot of chat session state
type ChatSessionSnapshot struct {
	RequestID    string    `json:"request_id"`
	Status       string    `json:"status"`
	Model        string    `json:"model"`
	StartTime    time.Time `json:"start_time"`
	IsFirstChunk bool      `json:"is_first_chunk"`
	HasToolCalls bool      `json:"has_tool_calls"`
	LastActivity time.Time `json:"last_activity"`
}

// ToolExecutionSnapshot represents a snapshot of tool execution state
type ToolExecutionSnapshot struct {
	Status         string            `json:"status"`
	TotalTools     int               `json:"total_tools"`
	CompletedTools int               `json:"completed_tools"`
	StartTime      time.Time         `json:"start_time"`
	CurrentTool    *ToolCallSnapshot `json:"current_tool,omitempty"`
}

// ToolCallSnapshot represents a snapshot of tool call state
type ToolCallSnapshot struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	StartTime time.Time `json:"start_time"`
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
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	Image     string     `json:"image"`
	State     AgentState `json:"state"`
	Message   string     `json:"message,omitempty"`
	StartTime time.Time  `json:"start_time"`
	ReadyTime *time.Time `json:"ready_time,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// AgentState represents the current state of an agent
type AgentState int

const (
	AgentStateUnknown AgentState = iota
	AgentStatePullingImage
	AgentStateStarting
	AgentStateWaitingReady
	AgentStateReady
	AgentStateFailed
)

func (a AgentState) String() string {
	switch a {
	case AgentStateUnknown:
		return "Unknown"
	case AgentStatePullingImage:
		return "PullingImage"
	case AgentStateStarting:
		return "Starting"
	case AgentStateWaitingReady:
		return "WaitingReady"
	case AgentStateReady:
		return "Ready"
	case AgentStateFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// DisplayName returns a user-friendly display name for the agent state
func (a AgentState) DisplayName() string {
	switch a {
	case AgentStateUnknown:
		return "unknown"
	case AgentStatePullingImage:
		return "pulling image"
	case AgentStateStarting:
		return "starting"
	case AgentStateWaitingReady:
		return "waiting"
	case AgentStateReady:
		return "ready"
	case AgentStateFailed:
		return "failed"
	default:
		return "unknown"
	}
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
func (s *ApplicationState) UpdateAgentStatus(name string, state AgentState, message string, url string, image string) {
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

	oldState := agent.State
	agent.State = state
	agent.Message = message

	// Update ready count
	if oldState != AgentStateReady && state == AgentStateReady {
		now := time.Now()
		agent.ReadyTime = &now
		s.agentReadiness.ReadyAgents++
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

	agent.State = AgentStateFailed
	agent.Error = err.Error()
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

	agent, exists := s.agentReadiness.Agents[name]
	if !exists {
		return
	}

	if agent.State == AgentStateReady {
		s.agentReadiness.ReadyAgents--
	}

	delete(s.agentReadiness.Agents, name)

	s.agentReadiness.TotalAgents--
}
