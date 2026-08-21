// Chat event stream contracts: the event interface the agent emits and the
// bridge that carries it to presentation layers.

package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

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

// EventBridgeManager handles event multicast for external event consumers
type EventBridgeManager interface {
	SetEventBridge(bridge EventBridge)
	GetEventBridge() EventBridge
	BroadcastEvent(event ChatEvent)
}
