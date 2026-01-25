package domain

import (
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// AgentEvent represents an event in the event-driven agent system
type AgentEvent interface {
	EventType() string
}

// MessageReceivedEvent is triggered when a new message arrives
type MessageReceivedEvent struct {
	Message sdk.Message
}

func (e MessageReceivedEvent) EventType() string { return "MessageReceived" }

// StreamCompletedEvent is triggered when LLM streaming completes
type StreamCompletedEvent struct {
	Message            sdk.Message
	ToolCalls          []*sdk.ChatCompletionMessageToolCall
	Reasoning          string
	Usage              *sdk.CompletionUsage
	IterationStartTime time.Time
}

func (e StreamCompletedEvent) EventType() string { return "StreamCompleted" }

// ToolsCompletedEvent is triggered when all tools finish executing
type ToolsCompletedEvent struct {
	Results []ConversationEntry
}

func (e ToolsCompletedEvent) EventType() string { return "ToolsCompleted" }

// CompletionRequestedEvent is triggered when the agent should complete
type CompletionRequestedEvent struct{}

func (e CompletionRequestedEvent) EventType() string { return "CompletionRequested" }

// StartStreamingEvent is triggered when the agent should start streaming
type StartStreamingEvent struct{}

func (e StartStreamingEvent) EventType() string { return "StartStreaming" }

// ProcessNextToolEvent is triggered to process the next tool in the queue
type ProcessNextToolEvent struct {
	ToolIndex int
}

func (e ProcessNextToolEvent) EventType() string { return "ProcessNextTool" }

// AllToolsProcessedEvent is triggered when all tools have been processed
type AllToolsProcessedEvent struct{}

func (e AllToolsProcessedEvent) EventType() string { return "AllToolsProcessed" }

// ApprovalFailedEvent is triggered when approval fails
type ApprovalFailedEvent struct {
	Error error
}

func (e ApprovalFailedEvent) EventType() string { return "ApprovalFailed" }

// StateHandler defines the interface for handling events in a specific state
type StateHandler interface {
	Handle(event AgentEvent) error
	Name() AgentExecutionState
}

// StateContext provides access to agent dependencies for state handlers
type StateContext struct {
	// Core dependencies
	StateMachine AgentStateMachine
	AgentCtx     *AgentContext

	// Event communication
	Events chan AgentEvent

	// Concurrency control
	WaitGroup  *sync.WaitGroup
	CancelChan <-chan struct{}
	Mutex      *sync.Mutex

	// Shared state data
	CurrentMessage   *sdk.Message
	CurrentToolCalls *[]*sdk.ChatCompletionMessageToolCall
	CurrentReasoning *string
	AvailableTools   *[]sdk.ChatCompletionTool

	// Tool processing state
	ToolsNeedingApproval *[]sdk.ChatCompletionMessageToolCall
	CurrentToolIndex     *int
	ToolResults          *[]ConversationEntry

	// Request context
	Request     *AgentRequest
	TaskTracker TaskTracker
	Provider    string
	Model       string

	// Function callbacks
	ToolExecutor   *func()
	StartStreaming func()

	// Helper methods - these will be implemented as methods that delegate to internal service
	GetMetrics            func(requestID string) *ChatMetrics
	ShouldRequireApproval func(toolCall *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool
	AddMessage            func(entry ConversationEntry) error
	BatchDrainQueue       func() int
	RequestToolApproval   func(toolCall sdk.ChatCompletionMessageToolCall) (bool, error)
	ExecuteToolInternal   func(toolCall sdk.ChatCompletionMessageToolCall, isApproved bool) ConversationEntry
	GetAgentMode          func() AgentMode
	PublishChatEvent      func(event ChatEvent)
	PublishChatComplete   func(reasoning string, toolCalls []sdk.ChatCompletionMessageToolCall, metrics *ChatMetrics)
}
