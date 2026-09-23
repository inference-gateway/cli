package tui

import (
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// UI Events for application state management
//
// All events in this file implement tea.Msg (Bubble Tea's message interface) and are part
// of the Bubble Tea message system. These events represent UI-specific operations like
// input handling, status updates, and navigation.

// UpdateHistoryEvent updates the conversation history display
type UpdateHistoryEvent struct {
	History []convdomain.ConversationEntry
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
	Submit     bool
}

// RolloverCompletedEvent is dispatched when an asynchronous auto-rollover
// finishes in chat mode. It carries the already-built sdk.Message + images
// that were pending while the summary LLM call was in flight, so the
// post-rollover handler can resume the deferred AddMessage + start-chat
// completion flow without re-parsing user input.
type RolloverCompletedEvent struct {
	Message sdk.Message
	Images  []agentdomain.ImageAttachment
}

// ThemeSelectedEvent indicates theme selection
type ThemeSelectedEvent struct {
	Theme string
}

// ConversationSelectedEvent indicates conversation selection
type ConversationSelectedEvent struct {
	ConversationID string
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

// Approval Events

// Plan Approval Events

// PlanApprovalResponseEvent captures the user's plan approval decision
type PlanApprovalResponseEvent struct {
	Action agentdomain.PlanApprovalAction
}

// Todo Events

// TodoUpdateEvent indicates the todo list has been updated
type TodoUpdateEvent struct {
	Todos []agentdomain.TodoItem
}

// ToggleTodoBoxEvent toggles the todo box expanded/collapsed state
type ToggleTodoBoxEvent struct{}

// GitPRResolvedEvent carries the PR number for the current branch, resolved
// asynchronously by the input view's fetch command. An empty PR means no PR
// exists (or gh is unavailable). Defined here rather than as a component-local
// msg because the chat application only routes domain-prefixed messages to UI
// components.
type GitPRResolvedEvent struct {
	PR string
}

// GitStatusResolvedEvent carries the workspace state for the branch icon,
// resolved asynchronously by the input view's fetch command. Dirty means the
// tree has uncommitted changes; Unpushed means local commits are ahead of the
// upstream (or there is no upstream). The zero value means clean or not a repo.
type GitStatusResolvedEvent struct {
	Dirty    bool
	Unpushed bool
}

// BashCommandCompletedEvent indicates a direct bash command (! prefix) has completed
type BashCommandCompletedEvent struct {
	History       []convdomain.ConversationEntry
	Failed        bool
	UserInitiated bool
	ErrorMessage  string
}

// DrainQueueRetryEvent is the bounded retry behind DrainQueueEvent, and is NOT a
// clock. A DrainQueueEvent can land while the agent is momentarily busy (e.g. a
// background job finishes in the same instant the turn is still completing); the
// gate drops it, so without a retry the queue would strand. HandleDrainQueueEvent
// arms a single DrainQueueRetryEvent in that case, and the drainRetryArmed guard
// keeps it to exactly one outstanding timer no matter how many DrainQueueEvents
// arrived. When it fires, HandleDrainQueueRetryEvent re-runs the gate, which
// re-arms only while work is still stranded and stops the moment the queue drains.
type DrainQueueRetryEvent struct{}

// Agent Readiness Events

// AgentStatusUpdateEvent indicates an agent's status has changed
type AgentStatusUpdateEvent struct {
	AgentName string
	State     agentdomain.AgentState
	Message   string
	URL       string
	Image     string
}

// MCP Server Status Events

// GitHub App Setup Events

// TriggerOpentaskSetupEvent triggers the GitHub App setup flow
type TriggerOpentaskSetupEvent struct{}

// TriggerHelpViewEvent opens the full-screen, scrollable help overlay that
// lists every slash command and keybinding in two tables.
type TriggerHelpViewEvent struct{}

// PlanApprovalSelectionChangedEvent signals that the plan-approval button
// selection has moved and the conversation viewport needs to re-render so
// the highlighted button reflects the new index.
type PlanApprovalSelectionChangedEvent struct {
	NewIndex int
}

// MessageHistoryReadyEvent indicates message history has been loaded and is ready to display
type MessageHistoryReadyEvent struct {
	Messages []MessageSnapshot
}

// MessageHistoryEditEvent is emitted when user wants to edit a selected message
type MessageHistoryEditEvent struct {
	RequestID       string
	Timestamp       time.Time
	MessageIndex    int
	MessageContent  string
	MessageSnapshot MessageSnapshot
}

func (e MessageHistoryEditEvent) GetRequestID() string    { return e.RequestID }
func (e MessageHistoryEditEvent) GetTimestamp() time.Time { return e.Timestamp }

// MessageHistoryEditReadyEvent indicates editing is ready to begin
type MessageHistoryEditReadyEvent struct {
	MessageIndex int
	Content      string
	Snapshot     MessageSnapshot
}

// ToolCallStreamStatus represents the status of a tool call during streaming
type ToolCallStreamStatus string

const (
	ToolCallStreamStatusStreaming ToolCallStreamStatus = "streaming"
	ToolCallStreamStatusComplete  ToolCallStreamStatus = "completed"
	ToolCallStreamStatusReady     ToolCallStreamStatus = "ready"
)

// ToolCallPreviewEvent shows a tool call as it's being streamed (before execution)
type ToolCallPreviewEvent struct {
	RequestID  string
	Timestamp  time.Time
	ToolCallID string
	ToolName   string
	Arguments  string
	Status     ToolCallStreamStatus
	IsComplete bool
}

func (e ToolCallPreviewEvent) GetRequestID() string    { return e.RequestID }
func (e ToolCallPreviewEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolApprovalNotificationEvent is sent to notify the Computer Use dialog when tool approval is required in TUI
type ToolApprovalNotificationEvent struct {
	RequestID string
	Timestamp time.Time
	ToolName  string
	Message   string
}

func (e ToolApprovalNotificationEvent) GetRequestID() string    { return e.RequestID }
func (e ToolApprovalNotificationEvent) GetTimestamp() time.Time { return e.Timestamp }

// RefreshAutocompleteEvent is sent when autocomplete needs to refresh (e.g., after mode change)
type RefreshAutocompleteEvent struct{}

// BackgroundShellRequestEvent requests that the current running Bash command be moved to background
type BackgroundShellRequestEvent struct{}

// NavigateBackInTimeEvent triggers the message history selector view
type NavigateBackInTimeEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e NavigateBackInTimeEvent) GetRequestID() string    { return e.RequestID }
func (e NavigateBackInTimeEvent) GetTimestamp() time.Time { return e.Timestamp }

// MessageHistoryRestoreEvent is emitted when user selects a restore point in message history
type MessageHistoryRestoreEvent struct {
	RequestID      string
	Timestamp      time.Time
	RestoreToIndex int
}

func (e MessageHistoryRestoreEvent) GetRequestID() string    { return e.RequestID }
func (e MessageHistoryRestoreEvent) GetTimestamp() time.Time { return e.Timestamp }

// MessageEditSubmitEvent is emitted when edited message is submitted
type MessageEditSubmitEvent struct {
	RequestID     string
	Timestamp     time.Time
	OriginalIndex int
	EditedContent string
	Images        []agentdomain.ImageAttachment
}

func (e MessageEditSubmitEvent) GetRequestID() string    { return e.RequestID }
func (e MessageEditSubmitEvent) GetTimestamp() time.Time { return e.Timestamp }

// HeartbeatEvent is the app's single periodic tick, pushed through the UI
// notifier by one background goroutine (cmd/chat) at a fixed slow interval. It
// exists so freshness checks that cannot be event-driven (state changed outside
// the TUI, e.g. git status after an editor save) have one clock to ride instead
// of each re-arming its own tea.Tick. Handlers must stay cheap: kick off a
// tea.Cmd for any I/O, never do it inline. Consumers that want a slower cadence
// compare At against their own last-run time.
type HeartbeatEvent struct {
	At time.Time
}
