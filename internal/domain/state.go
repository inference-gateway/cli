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
	fileSelectionState *FileSelectionState

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
	ViewStateTaskManagement
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
	case ViewStateTaskManagement:
		return "TaskManagement"
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

// FileSelectionState represents the state of file selection UI
type FileSelectionState struct {
	Files         []string `json:"files"`
	SearchQuery   string   `json:"search_query"`
	SelectedIndex int      `json:"selected_index"`
}

// NewApplicationState creates a new application state
func NewApplicationState() *ApplicationState {
	return &ApplicationState{
		currentView:        ViewStateModelSelection,
		previousView:       ViewStateModelSelection,
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
		},
		ViewStateFileSelection:         {ViewStateChat},
		ViewStateTextSelection:         {ViewStateChat},
		ViewStateConversationSelection: {ViewStateChat},
		ViewStateThemeSelection:        {ViewStateChat},
		ViewStateA2AServers:            {ViewStateChat},
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
