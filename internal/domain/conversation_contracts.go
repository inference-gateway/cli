// Conversation storage, session stats, chat events, and message-queue contracts.

package domain

import (
	"context"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// ConversationEntry represents a message in the conversation with metadata
type ConversationEntry struct {
	// Core message fields
	Message          Message           `json:"message"`
	Model            string            `json:"model,omitempty"`
	Time             time.Time         `json:"time"`
	Hidden           bool              `json:"hidden,omitempty"`
	Images           []ImageAttachment `json:"images,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`

	// Tool-related fields
	ToolExecution      *ToolExecutionResult               `json:"tool_execution,omitempty"`
	PendingToolCall    *sdk.ChatCompletionMessageToolCall `json:"pending_tool_call,omitempty"`
	ToolApprovalStatus ToolApprovalStatus                 `json:"tool_approval_status,omitempty"`

	// Plan mode fields
	Rejected           bool               `json:"rejected,omitempty"`
	IsPlan             bool               `json:"is_plan,omitempty"`
	PlanApprovalStatus PlanApprovalStatus `json:"plan_approval_status,omitempty"`
}

// PlanApprovalStatus represents the approval status of a plan
type PlanApprovalStatus int

const (
	PlanApprovalPending PlanApprovalStatus = iota
	PlanApprovalAccepted
	PlanApprovalRejected
)

// ToolApprovalStatus represents the approval status of a tool
type ToolApprovalStatus int

const (
	ToolApprovalPending ToolApprovalStatus = iota
	ToolApprovalApproved
	ToolApprovalRejected
)

// ExportFormat defines the format for exporting conversations
type ExportFormat string

const (
	ExportMarkdown ExportFormat = "markdown"
	ExportJSON     ExportFormat = "json"
	ExportText     ExportFormat = "text"
)

// SessionTokenStats tracks accumulated token usage across a session
type SessionTokenStats struct {
	TotalInputTokens      int `json:"total_input_tokens"`
	TotalOutputTokens     int `json:"total_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
	RequestCount          int `json:"request_count"`
	LastInputTokens       int `json:"last_input_tokens"`
	TotalCachedTokens     int `json:"total_cached_tokens"`
	TotalCacheWriteTokens int `json:"total_cache_write_tokens"`
}

// ConversationRepository is the composed interface for all conversation storage
// and retrieval operations. New code should depend on the narrower sub-interfaces above.
type ConversationRepository interface {
	AddMessage(msg ConversationEntry) error
	GetMessages() []ConversationEntry
	Clear() error
	ClearExceptFirstUserMessage() error
	GetMessageCount() int
	UpdateLastMessage(content string) error
	UpdateLastMessageToolCalls(toolCalls *[]sdk.ChatCompletionMessageToolCall) error
	DeleteMessagesAfterIndex(index int) error

	AddTokenUsage(model string, inputTokens, outputTokens, totalTokens, cachedTokens, cacheWriteTokens int) error
	AddCachedTokens(tokens int)
	GetSessionTokens() SessionTokenStats
	GetSessionCostStats() SessionCostStats

	FormatToolResultForLLM(result *ToolExecutionResult) string
	FormatToolResultForUI(result *ToolExecutionResult, terminalWidth int) string
	FormatToolResultExpanded(result *ToolExecutionResult, terminalWidth int) string

	RemovePendingToolCallByID(toolCallID string)

	StartNewConversation(title string) error
	LoadConversation(ctx context.Context, conversationID string) error
	GetCurrentConversationTitle() string
	GetCurrentConversationID() string

	Export(format ExportFormat) ([]byte, error)
}

// ConversationOptimizer optimizes conversation history to reduce token usage
type ConversationOptimizer interface {
	OptimizeMessages(messages []sdk.Message, model string, force bool) []sdk.Message
}

// ModelService handles model selection and information
type ModelService interface {
	ListModels(ctx context.Context) ([]string, error)
	SelectModel(modelID string) error
	GetCurrentModel() string
	IsModelAvailable(modelID string) bool
	ValidateModel(modelID string) error
}

// ChatEvent represents events during chat operations
type ChatEvent interface {
	GetRequestID() string
	GetTimestamp() time.Time
}

// EventBridge multicasts chat events to multiple subscribers (e.g., terminal UI and the opentask extension bridge)
type EventBridge interface {
	// Tap intercepts an event stream and multicasts it to all subscribers
	// Returns a new channel that mirrors the input channel
	Tap(input <-chan ChatEvent) <-chan ChatEvent

	// Publish broadcasts an event to all subscribers
	Publish(event ChatEvent)

	// Subscribe creates a new event channel and returns it
	Subscribe() chan ChatEvent

	// SubscribeFuture is Subscribe without the ring-buffer replay, for
	// subscribers that backfill history another way.
	SubscribeFuture() chan ChatEvent

	// Unsubscribe removes a subscriber and closes its channel
	Unsubscribe(ch chan ChatEvent)
}

// ChatMetrics holds performance and usage metrics
type ChatMetrics struct {
	Duration time.Duration
	Usage    *sdk.CompletionUsage
}

// ChatSyncResponse represents a synchronous chat completion response
type ChatSyncResponse struct {
	RequestID        string                              `json:"request_id"`
	Content          string                              `json:"content"`
	ReasoningContent string                              `json:"reasoning_content,omitempty"`
	ToolCalls        []sdk.ChatCompletionMessageToolCall `json:"tool_calls,omitempty"`
	Usage            *sdk.CompletionUsage                `json:"usage,omitempty"`
	Duration         time.Duration                       `json:"duration"`
	FinishReason     string                              `json:"finish_reason,omitempty"`
}

// MessageQueue handles centralized message queuing for all components
type MessageQueue interface {
	// Enqueue adds a message to the queue
	Enqueue(message Message, requestID string)

	// Dequeue removes and returns the next message from the queue
	// Returns nil if the queue is empty
	Dequeue() *QueuedMessage

	// Peek returns the next message without removing it
	// Returns nil if the queue is empty
	Peek() *QueuedMessage

	// Size returns the number of messages in the queue
	Size() int

	// IsEmpty returns true if the queue has no messages
	IsEmpty() bool

	// Clear removes all messages from the queue
	Clear()

	// GetAll returns all messages in the queue without removing them
	GetAll() []QueuedMessage
}
