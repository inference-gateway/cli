package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// All events in this file implement tea.Msg (Bubble Tea's message interface) and are part
// of the Bubble Tea message system. They can be passed directly through the Bubble Tea
// event loop without conversion, since tea.Msg is an empty interface marker.
//
// Event Lifecycle:
//   1. Events are created by services (agent, tools, etc.)
//   2. Events are sent through tea.Cmd functions
//   3. Components receive events via their Update(tea.Msg) method
//   4. Components handle events directly, no central dispatcher needed

// ToolCallStreamStatus represents the status of a tool call during streaming
type ToolCallStreamStatus string

const (
	ToolCallStreamStatusStreaming ToolCallStreamStatus = "streaming"
	ToolCallStreamStatusComplete  ToolCallStreamStatus = "completed"
	ToolCallStreamStatusReady     ToolCallStreamStatus = "ready"
)

// ChatStartEvent indicates a chat request has started
type ChatStartEvent struct {
	RequestID string
	Timestamp time.Time
	Model     string
}

func (e ChatStartEvent) GetRequestID() string    { return e.RequestID }
func (e ChatStartEvent) GetTimestamp() time.Time { return e.Timestamp }

// ChatChunkEvent represents a streaming chunk of chat response
type ChatChunkEvent struct {
	RequestID        string
	Timestamp        time.Time
	Content          string
	ReasoningContent string
	ToolCalls        []sdk.ChatCompletionMessageToolCallChunk
	Delta            bool
	Usage            *sdk.CompletionUsage
}

func (e ChatChunkEvent) GetRequestID() string    { return e.RequestID }
func (e ChatChunkEvent) GetTimestamp() time.Time { return e.Timestamp }

// ChatCompleteEvent indicates chat completion. Cancelled is set when the
// completion is the result of a user-initiated cancel (Esc) rather than the
// model finishing on its own - the UI uses this to show "User interrupted"
// rather than "Response complete".
type ChatCompleteEvent struct {
	RequestID        string
	Timestamp        time.Time
	Message          string
	ReasoningContent string
	ToolCalls        []sdk.ChatCompletionMessageToolCall
	Metrics          *ChatMetrics
	Cancelled        bool
}

func (e ChatCompleteEvent) GetRequestID() string    { return e.RequestID }
func (e ChatCompleteEvent) GetTimestamp() time.Time { return e.Timestamp }

// ChatErrorEvent represents an error during chat
type ChatErrorEvent struct {
	RequestID string
	Timestamp time.Time
	Error     error
}

func (e ChatErrorEvent) GetRequestID() string    { return e.RequestID }
func (e ChatErrorEvent) GetTimestamp() time.Time { return e.Timestamp }

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

// ToolCallUpdateEvent updates a streaming tool call with new content
type ToolCallUpdateEvent struct {
	RequestID  string
	Timestamp  time.Time
	ToolCallID string
	ToolName   string
	Arguments  string
	Status     ToolCallStreamStatus
}

func (e ToolCallUpdateEvent) GetRequestID() string    { return e.RequestID }
func (e ToolCallUpdateEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolCallReadyEvent indicates all tool calls are ready for approval/execution
type ToolCallReadyEvent struct {
	RequestID string
	Timestamp time.Time
	ToolCalls []sdk.ChatCompletionMessageToolCall
}

func (e ToolCallReadyEvent) GetRequestID() string    { return e.RequestID }
func (e ToolCallReadyEvent) GetTimestamp() time.Time { return e.Timestamp }

// OptimizationStatusEvent indicates conversation optimization status
type OptimizationStatusEvent struct {
	RequestID      string
	Timestamp      time.Time
	Message        string
	IsActive       bool
	OriginalCount  int
	OptimizedCount int
}

func (e OptimizationStatusEvent) GetRequestID() string    { return e.RequestID }
func (e OptimizationStatusEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2AToolCallExecutedEvent indicates an A2A tool call was executed on the gateway
type A2AToolCallExecutedEvent struct {
	RequestID         string
	Timestamp         time.Time
	ToolCallID        string
	ToolName          string
	Arguments         string
	ExecutedOnGateway bool
	TaskID            string
}

func (e A2AToolCallExecutedEvent) GetRequestID() string    { return e.RequestID }
func (e A2AToolCallExecutedEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2ATaskSubmittedEvent indicates an A2A task was submitted
type A2ATaskSubmittedEvent struct {
	RequestID string
	Timestamp time.Time
	TaskID    string
	AgentName string
	AgentURL  string
	TaskType  string
}

func (e A2ATaskSubmittedEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskSubmittedEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2ATaskStatusUpdateEvent indicates an A2A task status update
type A2ATaskStatusUpdateEvent struct {
	RequestID string
	Timestamp time.Time
	TaskID    string
	AgentURL  string
	Status    string
	Progress  float64
	Message   string
}

func (e A2ATaskStatusUpdateEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskStatusUpdateEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2ATaskCompletedEvent indicates an A2A task was completed successfully
type A2ATaskCompletedEvent struct {
	RequestID string
	Timestamp time.Time
	TaskID    string
	Result    ToolExecutionResult
}

func (e A2ATaskCompletedEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskCompletedEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2ATaskFailedEvent indicates an A2A task failed
type A2ATaskFailedEvent struct {
	RequestID string
	Timestamp time.Time
	TaskID    string
	Result    ToolExecutionResult
	Error     string
}

func (e A2ATaskFailedEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskFailedEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2ATaskInputRequiredEvent indicates an A2A task requires user input
type A2ATaskInputRequiredEvent struct {
	RequestID string
	Timestamp time.Time
	TaskID    string
	Message   string
	Required  bool
}

func (e A2ATaskInputRequiredEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskInputRequiredEvent) GetTimestamp() time.Time { return e.Timestamp }

// SubagentSubmittedEvent indicates a local subagent was dispatched
type SubagentSubmittedEvent struct {
	RequestID  string
	Timestamp  time.Time
	SubagentID string
	Label      string
}

func (e SubagentSubmittedEvent) GetRequestID() string    { return e.RequestID }
func (e SubagentSubmittedEvent) GetTimestamp() time.Time { return e.Timestamp }

// SubagentCompletedEvent indicates a local subagent completed successfully
type SubagentCompletedEvent struct {
	RequestID  string
	Timestamp  time.Time
	SubagentID string
	Label      string
	Result     ToolExecutionResult
}

func (e SubagentCompletedEvent) GetRequestID() string    { return e.RequestID }
func (e SubagentCompletedEvent) GetTimestamp() time.Time { return e.Timestamp }

// SubagentFailedEvent indicates a local subagent failed
type SubagentFailedEvent struct {
	RequestID  string
	Timestamp  time.Time
	SubagentID string
	Label      string
	Result     ToolExecutionResult
	Error      string
}

func (e SubagentFailedEvent) GetRequestID() string    { return e.RequestID }
func (e SubagentFailedEvent) GetTimestamp() time.Time { return e.Timestamp }

// MessageQueuedEvent indicates a message was received from the queue and stored
type MessageQueuedEvent struct {
	RequestID string
	Timestamp time.Time
	Message   sdk.Message
}

func (e MessageQueuedEvent) GetRequestID() string    { return e.RequestID }
func (e MessageQueuedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolApprovalRequestedEvent is used for standard tool approval workflow.
// Computer-use tools use a separate pause/resume mechanism.
type ToolApprovalRequestedEvent struct {
	RequestID    string
	Timestamp    time.Time
	ToolCall     sdk.ChatCompletionMessageToolCall
	ResponseChan chan ApprovalAction `json:"-"`
}

func (e ToolApprovalRequestedEvent) GetRequestID() string    { return e.RequestID }
func (e ToolApprovalRequestedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolCancelledEvent is published when the conversation validator
// synthesizes a Tool-role response for an assistant tool_call whose
// real execution never completed (typically because the user pressed
// Esc between the model emitting tool_calls and the tools running).
// The conversation view uses this to surface a "[cancelled]" entry
// so the user understands why a requested tool never produced output.
type ToolCancelledEvent struct {
	RequestID  string
	Timestamp  time.Time
	ToolCallID string
	ToolName   string
}

func (e ToolCancelledEvent) GetRequestID() string    { return e.RequestID }
func (e ToolCancelledEvent) GetTimestamp() time.Time { return e.Timestamp }

// ComputerUsePausedEvent indicates computer-use execution has been paused
type ComputerUsePausedEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e ComputerUsePausedEvent) GetRequestID() string    { return e.RequestID }
func (e ComputerUsePausedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ComputerUseResumedEvent indicates computer-use execution has resumed
type ComputerUseResumedEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e ComputerUseResumedEvent) GetRequestID() string    { return e.RequestID }
func (e ComputerUseResumedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolApprovalNotificationEvent is sent to notify the Computer Use dialog when tool approval is required in TUI
type ToolApprovalNotificationEvent struct {
	RequestID string
	Timestamp time.Time
	ToolName  string
	Message   string
}

func (e ToolApprovalNotificationEvent) GetRequestID() string    { return e.RequestID }
func (e ToolApprovalNotificationEvent) GetTimestamp() time.Time { return e.Timestamp }

// PlanApprovalRequestedEvent indicates plan mode completion requires user approval
type PlanApprovalRequestedEvent struct {
	RequestID    string
	Timestamp    time.Time
	PlanContent  string
	PlanID       string
	ResponseChan chan PlanApprovalAction `json:"-"`
}

func (e PlanApprovalRequestedEvent) GetRequestID() string    { return e.RequestID }
func (e PlanApprovalRequestedEvent) GetTimestamp() time.Time { return e.Timestamp }

// UserQuestionRequestedEvent is published when the AskUserQuestion tool asks the
// user one or more interactive clarifying questions. ResponseChan delivers the
// collected answers back to the blocked tool goroutine; closing it without a
// value signals cancellation.
type UserQuestionRequestedEvent struct {
	RequestID    string
	Timestamp    time.Time
	Questions    []UserQuestion
	ResponseChan chan []UserQuestionAnswer `json:"-"`
}

func (e UserQuestionRequestedEvent) GetRequestID() string    { return e.RequestID }
func (e UserQuestionRequestedEvent) GetTimestamp() time.Time { return e.Timestamp }

// RefreshAutocompleteEvent is sent when autocomplete needs to refresh (e.g., after mode change)
type RefreshAutocompleteEvent struct{}

// BackgroundShellRequestEvent requests that the current running Bash command be moved to background
type BackgroundShellRequestEvent struct{}

// ShellDetachedEvent indicates a Bash command has been moved to background
type ShellDetachedEvent struct {
	RequestID string
	Timestamp time.Time
	ShellID   string
	Command   string
}

func (e ShellDetachedEvent) GetRequestID() string    { return e.RequestID }
func (e ShellDetachedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ShellCompletedEvent indicates a background shell finished successfully
type ShellCompletedEvent struct {
	RequestID string
	Timestamp time.Time
	ShellID   string
	ExitCode  int
	Duration  time.Duration
}

func (e ShellCompletedEvent) GetRequestID() string    { return e.RequestID }
func (e ShellCompletedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ShellFailedEvent indicates a background shell failed
type ShellFailedEvent struct {
	RequestID string
	Timestamp time.Time
	ShellID   string
	Error     string
	ExitCode  int
}

func (e ShellFailedEvent) GetRequestID() string    { return e.RequestID }
func (e ShellFailedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ShellCancelledEvent indicates a background shell was killed
type ShellCancelledEvent struct {
	RequestID string
	Timestamp time.Time
	ShellID   string
}

func (e ShellCancelledEvent) GetRequestID() string    { return e.RequestID }
func (e ShellCancelledEvent) GetTimestamp() time.Time { return e.Timestamp }

// NavigateBackInTimeEvent triggers the message history selector view
type NavigateBackInTimeEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e NavigateBackInTimeEvent) GetRequestID() string    { return e.RequestID }
func (e NavigateBackInTimeEvent) GetTimestamp() time.Time { return e.Timestamp }

// MessageHistoryReadyEvent indicates message history has been loaded and is ready to display
type MessageHistoryReadyEvent struct {
	Messages []MessageSnapshot
}

// MessageHistoryRestoreEvent is emitted when user selects a restore point in message history
type MessageHistoryRestoreEvent struct {
	RequestID      string
	Timestamp      time.Time
	RestoreToIndex int
}

func (e MessageHistoryRestoreEvent) GetRequestID() string    { return e.RequestID }
func (e MessageHistoryRestoreEvent) GetTimestamp() time.Time { return e.Timestamp }

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

// MessageEditSubmitEvent is emitted when edited message is submitted
type MessageEditSubmitEvent struct {
	RequestID     string
	Timestamp     time.Time
	OriginalIndex int
	EditedContent string
	Images        []ImageAttachment
}

func (e MessageEditSubmitEvent) GetRequestID() string    { return e.RequestID }
func (e MessageEditSubmitEvent) GetTimestamp() time.Time { return e.Timestamp }
