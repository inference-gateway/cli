package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// UI Events for application state management
//
// All events in this file implement tea.Msg (Bubble Tea's message interface) and are part
// of the Bubble Tea message system. These events represent UI-specific operations like
// input handling, status updates, and navigation.

// UpdateHistoryEvent updates the conversation history display
type UpdateHistoryEvent struct {
	History []ConversationEntry
}

// StreamingContentEvent delivers live streaming content for immediate UI display
type StreamingContentEvent struct {
	RequestID        string
	Content          string
	ReasoningContent string
	Delta            bool
	Model            string
}

// SetStatusEvent sets a status message
type SetStatusEvent struct {
	Message    string
	Spinner    bool
	StatusType StatusType
	Progress   *StatusProgress
	ToolName   string
}

// UpdateStatusEvent updates an existing status message without resetting timer
type UpdateStatusEvent struct {
	Message    string
	StatusType StatusType
	ToolName   string
}

// ShowErrorEvent displays an error message
type ShowErrorEvent struct {
	Error  string
	Sticky bool // Whether error persists until dismissed
}

// ClearErrorEvent clears any displayed error
type ClearErrorEvent struct{}

// SaveStatusStateEvent saves the current status state for later restoration
type SaveStatusStateEvent struct{}

// RestoreStatusStateEvent restores a previously saved status state
type RestoreStatusStateEvent struct{}

// ClearInputEvent clears the input field
type ClearInputEvent struct{}

// SetInputEvent sets text in the input field
type SetInputEvent struct {
	Text string
}

// FocusStatusBarEvent moves keyboard focus to the status-indicator row below
// the input, fired when arrow-down would otherwise be a no-op
type FocusStatusBarEvent struct{}

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
	CursorPos  int
}

// UserInputEvent represents user input submission
type UserInputEvent struct {
	Content string
	Images  []ImageAttachment
}

// RolloverCompletedEvent is dispatched when an asynchronous auto-rollover
// finishes in chat mode. It carries the already-built sdk.Message + images
// that were pending while the summary LLM call was in flight, so the
// post-rollover handler can resume the deferred AddMessage + start-chat
// completion flow without re-parsing user input.
type RolloverCompletedEvent struct {
	Message sdk.Message
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
	Conversations []any
	Error         error
}

// Task Management Events

// TasksLoadedEvent indicates tasks have been loaded
type TasksLoadedEvent struct {
	ActiveTasks    []any
	CompletedTasks []any
	Error          error
}

// TaskCancelledEvent indicates a task has been cancelled
type TaskCancelledEvent struct {
	TaskID string
	Error  error
}

// Tool Execution Events

// ToolExecutionStartedEvent indicates tool execution has started
type ToolExecutionStartedEvent struct {
	SessionID  string
	TotalTools int
}

// ToolExecutionCompletedEvent indicates tool execution is complete
type ToolExecutionCompletedEvent struct {
	SessionID     string
	RequestID     string
	Timestamp     time.Time
	TotalExecuted int
	SuccessCount  int
	FailureCount  int
	Results       []*ToolExecutionResult
}

func (e ToolExecutionCompletedEvent) GetRequestID() string    { return e.RequestID }
func (e ToolExecutionCompletedEvent) GetTimestamp() time.Time { return e.Timestamp }

// Approval Events

// ToolApprovalResponseEvent captures the user's approval decision
type ToolApprovalResponseEvent struct {
	Action   ApprovalAction
	ToolCall sdk.ChatCompletionMessageToolCall
}

// Plan Approval Events

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
	History       []ConversationEntry
	Failed        bool
	UserInitiated bool
	ErrorMessage  string
}

// DrainQueueEvent asks the orchestrator to start a fresh agent turn when the
// agent is idle on the chat view and the shared message queue has content
// (background-job completion notes or user messages typed while busy). Unlike the
// old queue-drain tick it is not a clock: it is pushed exactly once per real
// trigger (a background job landing work, a turn completing with a non-empty
// queue, or re-entering the chat view), and HandleDrainQueueEvent is a pure gate
// that starts a turn (Idle -> CheckingQueue -> ... -> Completing -> Idle) or
// returns nil. There is no self-reschedule.
type DrainQueueEvent struct{}

// DrainQueueRetryEvent is the bounded retry behind DrainQueueEvent, and is NOT a
// clock. A DrainQueueEvent can land while the agent is momentarily busy (e.g. a
// background job finishes in the same instant the turn is still completing); the
// gate drops it, so without a retry the queue would strand. HandleDrainQueueEvent
// arms a single DrainQueueRetryEvent in that case, and the drainRetryArmed guard
// keeps it to exactly one outstanding timer no matter how many DrainQueueEvents
// arrived. When it fires, HandleDrainQueueRetryEvent re-runs the gate, which
// re-arms only while work is still stranded and stops the moment the queue drains.
type DrainQueueRetryEvent struct{}

// BackgroundTasksChangedEvent signals that a background job's status changed
// (submitted, signalled, completed, or failed). The supervisor pushes it so the
// /tasks view and the inline conversation rows refresh on real change instead of
// polling at render time.
type BackgroundTasksChangedEvent struct{}

// Agent Readiness Events

// AgentStatusUpdateEvent indicates an agent's status has changed
type AgentStatusUpdateEvent struct {
	AgentName string
	State     AgentState
	Message   string
	URL       string
	Image     string
}

// MCP Server Status Events

// MCPServerStatusUpdateEvent indicates MCP server status has changed
type MCPServerStatusUpdateEvent struct {
	ServerName       string
	Connected        bool
	TotalServers     int
	ConnectedServers int
	TotalTools       int
	Tools            []MCPDiscoveredTool
}

// GitHub App Setup Events

// TriggerGithubActionSetupEvent triggers the GitHub App setup flow
type TriggerGithubActionSetupEvent struct{}

// TriggerHelpViewEvent opens the full-screen, scrollable help overlay that
// lists every slash command and keybinding in two tables.
type TriggerHelpViewEvent struct{}

// ApprovalSelectionChangedEvent signals that the approval selection index has changed
// and the UI needs to refresh to show the new selection
type ApprovalSelectionChangedEvent struct {
	NewIndex int
}

// PlanApprovalSelectionChangedEvent signals that the plan-approval button
// selection has moved and the conversation viewport needs to re-render so
// the highlighted button reflects the new index.
type PlanApprovalSelectionChangedEvent struct {
	NewIndex int
}
