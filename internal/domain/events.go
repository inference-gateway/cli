package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
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
	RequestID string
	Timestamp time.Time
	Content   string
	ToolCalls []sdk.ChatCompletionMessageToolCall
	Delta     bool // true if this is a delta update
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

// ToolCallEvent represents a tool call request
type ToolCallEvent struct {
	RequestID string
	Timestamp time.Time
	ToolName  string
	Args      string
}

func (e ToolCallEvent) GetType() ChatEventType  { return EventToolCall }
func (e ToolCallEvent) GetRequestID() string    { return e.RequestID }
func (e ToolCallEvent) GetTimestamp() time.Time { return e.Timestamp }

// CancelledEvent indicates a request was cancelled
type CancelledEvent struct {
	RequestID string
	Timestamp time.Time
	Reason    string
}

func (e CancelledEvent) GetType() ChatEventType  { return EventCancelled }
func (e CancelledEvent) GetRequestID() string    { return e.RequestID }
func (e CancelledEvent) GetTimestamp() time.Time { return e.Timestamp }
