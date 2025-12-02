package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// ToolCallStreamStatus represents the status of a tool call during streaming
type ToolCallStreamStatus string

const (
	ToolCallStreamStatusStreaming ToolCallStreamStatus = "streaming"
	ToolCallStreamStatusComplete  ToolCallStreamStatus = "complete"
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

// ChatCompleteEvent indicates chat completion
type ChatCompleteEvent struct {
	RequestID string
	Timestamp time.Time
	Message   string
	ToolCalls []sdk.ChatCompletionMessageToolCall
	Metrics   *ChatMetrics
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

// CancelledEvent indicates a request was cancelled
type CancelledEvent struct {
	RequestID string
	Timestamp time.Time
	Reason    string
}

func (e CancelledEvent) GetRequestID() string    { return e.RequestID }
func (e CancelledEvent) GetTimestamp() time.Time { return e.Timestamp }

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
	TaskType  string
}

func (e A2ATaskSubmittedEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskSubmittedEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2ATaskStatusUpdateEvent indicates an A2A task status update
type A2ATaskStatusUpdateEvent struct {
	RequestID string
	Timestamp time.Time
	TaskID    string
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

// MessageQueuedEvent indicates a message was received from the queue and stored
type MessageQueuedEvent struct {
	RequestID string
	Timestamp time.Time
	Message   sdk.Message
}

func (e MessageQueuedEvent) GetRequestID() string    { return e.RequestID }
func (e MessageQueuedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolApprovalRequestedEvent indicates a tool requires user approval before execution
type ToolApprovalRequestedEvent struct {
	RequestID    string
	Timestamp    time.Time
	ToolCall     sdk.ChatCompletionMessageToolCall
	ResponseChan chan ApprovalAction
}

func (e ToolApprovalRequestedEvent) GetRequestID() string    { return e.RequestID }
func (e ToolApprovalRequestedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolApprovedEvent indicates the user approved the tool execution
type ToolApprovedEvent struct {
	RequestID string
	Timestamp time.Time
	ToolCall  sdk.ChatCompletionMessageToolCall
}

func (e ToolApprovedEvent) GetRequestID() string    { return e.RequestID }
func (e ToolApprovedEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolRejectedEvent indicates the user rejected the tool execution
type ToolRejectedEvent struct {
	RequestID string
	Timestamp time.Time
	ToolCall  sdk.ChatCompletionMessageToolCall
}

func (e ToolRejectedEvent) GetRequestID() string    { return e.RequestID }
func (e ToolRejectedEvent) GetTimestamp() time.Time { return e.Timestamp }

// PlanApprovalRequestedEvent indicates plan mode completion requires user approval
type PlanApprovalRequestedEvent struct {
	RequestID    string
	Timestamp    time.Time
	PlanContent  string
	ResponseChan chan PlanApprovalAction
}

func (e PlanApprovalRequestedEvent) GetRequestID() string    { return e.RequestID }
func (e PlanApprovalRequestedEvent) GetTimestamp() time.Time { return e.Timestamp }

// PlanApprovedEvent indicates the user approved the plan
type PlanApprovedEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e PlanApprovedEvent) GetRequestID() string    { return e.RequestID }
func (e PlanApprovedEvent) GetTimestamp() time.Time { return e.Timestamp }

// PlanApprovedAndAutoAcceptEvent indicates the user approved the plan and wants to enable auto-accept
type PlanApprovedAndAutoAcceptEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e PlanApprovedAndAutoAcceptEvent) GetRequestID() string    { return e.RequestID }
func (e PlanApprovedAndAutoAcceptEvent) GetTimestamp() time.Time { return e.Timestamp }

// PlanRejectedEvent indicates the user rejected the plan
type PlanRejectedEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e PlanRejectedEvent) GetRequestID() string    { return e.RequestID }
func (e PlanRejectedEvent) GetTimestamp() time.Time { return e.Timestamp }

// RefreshAutocompleteEvent is sent when autocomplete needs to refresh (e.g., after mode change)
type RefreshAutocompleteEvent struct{}
