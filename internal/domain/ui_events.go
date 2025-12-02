package domain

import (
	sdk "github.com/inference-gateway/sdk"
)

// UI Events for application state management

// UpdateHistoryEvent updates the conversation history display
type UpdateHistoryEvent struct {
	History []ConversationEntry
}

// StreamingContentEvent delivers live streaming content for immediate UI display
type StreamingContentEvent struct {
	RequestID string
	Content   string
	Delta     bool
}

// SetStatusEvent sets a status message
type SetStatusEvent struct {
	Message    string
	Spinner    bool
	StatusType StatusType
	Progress   *StatusProgress
}

// UpdateStatusEvent updates an existing status message without resetting timer
type UpdateStatusEvent struct {
	Message    string
	StatusType StatusType
}

// ShowErrorEvent displays an error message
type ShowErrorEvent struct {
	Error  string
	Sticky bool // Whether error persists until dismissed
}

// ClearErrorEvent clears any displayed error
type ClearErrorEvent struct{}

// ClearInputEvent clears the input field
type ClearInputEvent struct{}

// SetInputEvent sets text in the input field
type SetInputEvent struct {
	Text string
}

// AutocompleteUpdateEvent is fired when input text changes and autocomplete should update
type AutocompleteUpdateEvent struct {
	Text      string
	CursorPos int
}

// AutocompleteHideEvent is fired when autocomplete should be hidden
type AutocompleteHideEvent struct{}

// AutocompleteCompleteEvent is fired when a completion is selected
type AutocompleteCompleteEvent struct {
	Completion string
}

// AutocompleteVisibilityCheckEvent requests autocomplete visibility state
type AutocompleteVisibilityCheckEvent struct {
	ResponseChan chan bool
}

// UserInputEvent represents user input submission
type UserInputEvent struct {
	Content string
	Images  []ImageAttachment
}

// ModelSelectedEvent indicates model selection
type ModelSelectedEvent struct {
	Model string
}

// ThemeSelectedEvent indicates theme selection
type ThemeSelectedEvent struct {
	Theme string
}

// ConversationSelectedEvent indicates conversation selection
type ConversationSelectedEvent struct {
	ConversationID string
}

// InitializeConversationSelectionEvent indicates conversation selection view should be initialized
type InitializeConversationSelectionEvent struct{}

// FileSelectedEvent indicates file selection
type FileSelectedEvent struct {
	FilePath string
}

// FileSelectionRequestEvent requests file selection UI
type FileSelectionRequestEvent struct{}

// SetupFileSelectionEvent sets up file selection state with files
type SetupFileSelectionEvent struct {
	Files []string
}

// ScrollRequestEvent requests scrolling in a component
type ScrollRequestEvent struct {
	ComponentID string
	Direction   ScrollDirection
	Amount      int
}

// FocusRequestEvent requests focus change
type FocusRequestEvent struct {
	ComponentID string
}

// ResizeEvent handles terminal resize
type ResizeEvent struct {
	Width  int
	Height int
}

// DebugKeyEvent provides debug information about key presses
type DebugKeyEvent struct {
	Key     string
	Handler string
}

// ToggleHelpBarEvent toggles the help bar visibility
type ToggleHelpBarEvent struct{}

// HideHelpBarEvent hides the help bar when typing other characters
type HideHelpBarEvent struct{}

// ConversationsLoadedEvent indicates conversations have been loaded
type ConversationsLoadedEvent struct {
	Conversations []interface{}
	Error         error
}

// Task Management Events

// TasksLoadedEvent indicates tasks have been loaded
type TasksLoadedEvent struct {
	ActiveTasks    []interface{}
	CompletedTasks []interface{}
	Error          error
}

// TaskCancelledEvent indicates a task has been cancelled
type TaskCancelledEvent struct {
	TaskID string
	Error  error
}

// InitializeA2ATaskManagementEvent indicates A2A task management view should be initialized
type InitializeA2ATaskManagementEvent struct{}

// Tool Execution Events

// ToolExecutionStartedEvent indicates tool execution has started
type ToolExecutionStartedEvent struct {
	SessionID  string
	TotalTools int
}

// ToolExecutionCompletedEvent indicates tool execution is complete
type ToolExecutionCompletedEvent struct {
	SessionID     string
	TotalExecuted int
	SuccessCount  int
	FailureCount  int
	Results       []*ToolExecutionResult
}

// Approval Events

// ToolApprovalResponseEvent captures the user's approval decision
type ToolApprovalResponseEvent struct {
	Action   ApprovalAction
	ToolCall sdk.ChatCompletionMessageToolCall
}

// Plan Approval Events

// ShowPlanApprovalEvent displays the plan approval modal
type ShowPlanApprovalEvent struct {
	PlanContent  string
	ResponseChan chan PlanApprovalAction
}

// PlanApprovalResponseEvent captures the user's plan approval decision
type PlanApprovalResponseEvent struct {
	Action PlanApprovalAction
}

// Todo Events

// TodoUpdateEvent indicates the todo list has been updated
type TodoUpdateEvent struct {
	Todos []TodoItem
}

// ToggleTodoBoxEvent toggles the todo box expanded/collapsed state
type ToggleTodoBoxEvent struct{}

// BashCommandCompletedEvent indicates a direct bash command (! prefix) has completed
type BashCommandCompletedEvent struct {
	History []ConversationEntry
}

// Agent Readiness Events

// AgentStatusUpdateEvent indicates an agent's status has changed
type AgentStatusUpdateEvent struct {
	AgentName string
	State     AgentState
	Message   string
	URL       string
	Image     string
}

// AgentReadyEvent indicates an agent has become ready
type AgentReadyEvent struct {
	AgentName   string
	ReadyAgents int
	TotalAgents int
}

// AgentErrorEvent indicates an agent has encountered an error
type AgentErrorEvent struct {
	AgentName string
	Error     error
}

// GitHub App Setup Events

// TriggerGitHubAppSetupEvent triggers the GitHub App setup flow
type TriggerGitHubAppSetupEvent struct{}
