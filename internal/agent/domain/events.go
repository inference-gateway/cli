package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// Chat events the agent publishes on its ChatEvent stream. Delivery surfaces
// (TUI, headless renderers, the browser bridge) subscribe to the stream; the
// domain does not know how they render.

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
	// MaxTurnsReached marks a completion forced by the turn limit rather than
	// the task finishing; headless renderers map it to ErrMaxTurnsReached.
	MaxTurnsReached bool
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
	RequestID string
	Timestamp time.Time
	ToolCall  sdk.ChatCompletionMessageToolCall
	// Context is optional text shown above the call in the approval prompt.
	// RequestApproval escalations use it for the judge's reason and the
	// agent's justification; regular approvals leave it empty.
	Context      string
	ResponseChan chan ApprovalAction `json:"-"`
}

func (e ToolApprovalRequestedEvent) GetRequestID() string    { return e.RequestID }
func (e ToolApprovalRequestedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolApprovalResolvedEvent signals that a tool approval was answered (terminal
// or panel), so bus subscribers like the extension bridge clear their card. It is
// the reliable "answered" signal, replacing the racy next-event heuristic.
type ToolApprovalResolvedEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e ToolApprovalResolvedEvent) GetRequestID() string    { return e.RequestID }
func (e ToolApprovalResolvedEvent) GetTimestamp() time.Time { return e.Timestamp }

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
	ToolCallID   string
	Timestamp    time.Time
	Questions    []UserQuestion
	ResponseChan chan []UserQuestionAnswer `json:"-"`
}

func (e UserQuestionRequestedEvent) GetRequestID() string    { return e.RequestID }
func (e UserQuestionRequestedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ShellDetachedEvent indicates a Bash command has been moved to background
type ShellDetachedEvent struct {
	RequestID string
	Timestamp time.Time
	ShellID   string
	Command   string
}

func (e ShellDetachedEvent) GetRequestID() string    { return e.RequestID }
func (e ShellDetachedEvent) GetTimestamp() time.Time { return e.Timestamp }
