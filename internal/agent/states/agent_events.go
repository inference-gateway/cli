package states

import (
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
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

// ToolsCompletedEvent is triggered when all tools finish executing. Stop is set
// when the results signal the loop should terminate (a rejected tool or a
// successful RequestPlanApproval); the ExecutingTools state then routes to the
// Stopped terminal instead of continuing to PostToolExecution.
type ToolsCompletedEvent struct {
	Results []convdomain.ConversationEntry
	Stop    bool
}

func (e ToolsCompletedEvent) EventType() string { return "ToolsCompleted" }

// CompletionRequestedEvent is triggered when the agent should complete
type CompletionRequestedEvent struct{}

func (e CompletionRequestedEvent) EventType() string { return "CompletionRequested" }

// StartStreamingEvent is triggered when the agent should start streaming
type StartStreamingEvent struct{}

func (e StartStreamingEvent) EventType() string { return "StartStreaming" }

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
	WaitGroup *sync.WaitGroup
	Mutex     *sync.Mutex

	// Shared state data
	CurrentMessage   *sdk.Message
	CurrentToolCalls *[]*sdk.ChatCompletionMessageToolCall
	CurrentReasoning *string

	// Tool processing state
	ToolsNeedingApproval *[]sdk.ChatCompletionMessageToolCall
	CurrentToolIndex     *int
	ToolResults          *[]convdomain.ConversationEntry

	// Request context
	Request                *agentdomain.AgentRequest
	BackgroundTaskRegistry scheddomain.BackgroundTaskRegistry
	Provider               string
	Model                  string

	// MaxConcurrentTools bounds how many approved tools may execute concurrently
	// while later tools are still being approved.
	MaxConcurrentTools int

	// Function callbacks
	ToolExecutor   *func()
	StartStreaming func()

	// Helper methods - these will be implemented as methods that delegate to internal service
	GetMetrics            func(requestID string) *agentdomain.ChatMetrics
	ShouldRequireApproval func(toolCall *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool
	ApprovalDelivery      func(toolCall *sdk.ChatCompletionMessageToolCall) string
	AddMessage            func(entry convdomain.ConversationEntry) error
	BatchDrainQueue       func() int
	RequestToolApproval   func(toolCall sdk.ChatCompletionMessageToolCall) (bool, string, error)
	ExecuteToolInternal   func(toolCall sdk.ChatCompletionMessageToolCall, isApproved bool) convdomain.ConversationEntry
	GetAgentMode          func() agentdomain.AgentMode
	PublishChatEvent      func(event agentdomain.ChatEvent)
	PublishChatComplete   func(reasoning string, toolCalls []sdk.ChatCompletionMessageToolCall, metrics *agentdomain.ChatMetrics)
	PublishChatCancelled  func(metrics *agentdomain.ChatMetrics)
	PublishToolResults    func(results []convdomain.ConversationEntry)

	// DispatchHooks runs the actions attached to a hook point. State executors call it
	// at their loop point; the streaming path calls the service directly.
	DispatchHooks func(hook agentdomain.HookPoint)

	// WaitForBackgroundTasks blocks until in-flight background work quiesces or
	// posts a result to the message queue. Only non-chat runs invoke it, at the
	// completion boundary in CheckingQueue.
	WaitForBackgroundTasks func()
}
