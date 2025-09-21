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

func (e ChatStartEvent) GetType() ChatEventType  { return EventChatStart }
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
	Usage            *sdk.CompletionUsage // Live token usage during streaming
}

func (e ChatChunkEvent) GetType() ChatEventType  { return EventChatChunk }
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

func (e ChatCompleteEvent) GetType() ChatEventType  { return EventChatComplete }
func (e ChatCompleteEvent) GetRequestID() string    { return e.RequestID }
func (e ChatCompleteEvent) GetTimestamp() time.Time { return e.Timestamp }

// ChatErrorEvent represents an error during chat
type ChatErrorEvent struct {
	RequestID string
	Timestamp time.Time
	Error     error
}

func (e ChatErrorEvent) GetType() ChatEventType  { return EventChatError }
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

func (e ToolCallPreviewEvent) GetType() ChatEventType  { return EventToolCallPreview }
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

func (e ToolCallUpdateEvent) GetType() ChatEventType  { return EventToolCallUpdate }
func (e ToolCallUpdateEvent) GetRequestID() string    { return e.RequestID }
func (e ToolCallUpdateEvent) GetTimestamp() time.Time { return e.Timestamp }

// ToolCallReadyEvent indicates all tool calls are ready for approval/execution
type ToolCallReadyEvent struct {
	RequestID string
	Timestamp time.Time
	ToolCalls []sdk.ChatCompletionMessageToolCall
}

func (e ToolCallReadyEvent) GetType() ChatEventType  { return EventToolCallReady }
func (e ToolCallReadyEvent) GetRequestID() string    { return e.RequestID }
func (e ToolCallReadyEvent) GetTimestamp() time.Time { return e.Timestamp }

// CancelledEvent indicates a request was cancelled
type CancelledEvent struct {
	RequestID string
	Timestamp time.Time
	Reason    string
}

func (e CancelledEvent) GetType() ChatEventType  { return EventCancelled }
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

func (e OptimizationStatusEvent) GetType() ChatEventType  { return EventOptimizationStatus }
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

func (e A2AToolCallExecutedEvent) GetType() ChatEventType  { return EventA2AToolCallExecuted }
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

func (e A2ATaskSubmittedEvent) GetType() ChatEventType  { return EventA2ATaskSubmitted }
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

func (e A2ATaskStatusUpdateEvent) GetType() ChatEventType  { return EventA2ATaskStatusUpdate }
func (e A2ATaskStatusUpdateEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskStatusUpdateEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2ATaskCompletedEvent indicates an A2A task was completed
type A2ATaskCompletedEvent struct {
	RequestID string
	Timestamp time.Time
	TaskID    string
	Success   bool
	Result    interface{}
	Error     string
}

func (e A2ATaskCompletedEvent) GetType() ChatEventType  { return EventA2ATaskCompleted }
func (e A2ATaskCompletedEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskCompletedEvent) GetTimestamp() time.Time { return e.Timestamp }

// A2ATaskInputRequiredEvent indicates an A2A task requires user input
type A2ATaskInputRequiredEvent struct {
	RequestID string
	Timestamp time.Time
	TaskID    string
	Message   string
	Required  bool
}

func (e A2ATaskInputRequiredEvent) GetType() ChatEventType  { return EventA2ATaskInputRequired }
func (e A2ATaskInputRequiredEvent) GetRequestID() string    { return e.RequestID }
func (e A2ATaskInputRequiredEvent) GetTimestamp() time.Time { return e.Timestamp }

// BackgroundTaskStartedEvent indicates a background task was started
type BackgroundTaskStartedEvent struct {
	TaskID      string
	AgentURL    string
	Description string
	Timestamp   time.Time
}

func (e BackgroundTaskStartedEvent) GetType() UIEventType    { return UIEventBackgroundTaskStarted }
func (e BackgroundTaskStartedEvent) GetTimestamp() time.Time { return e.Timestamp }

// BackgroundTaskCompletedEvent indicates a background task was completed
type BackgroundTaskCompletedEvent struct {
	TaskID    string
	Success   bool
	Result    string
	Error     string
	Timestamp time.Time
}

func (e BackgroundTaskCompletedEvent) GetType() UIEventType    { return UIEventBackgroundTaskCompleted }
func (e BackgroundTaskCompletedEvent) GetTimestamp() time.Time { return e.Timestamp }
