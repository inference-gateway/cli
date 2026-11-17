package states

import (
	"sync"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// AgentEvent represents an event in the event-driven agent system
type AgentEvent interface {
	EventType() string
}

// MessageReceivedEvent is triggered when a new message arrives
type MessageReceivedEvent struct{}

func (e MessageReceivedEvent) EventType() string { return "MessageReceived" }

// StreamCompletedEvent is triggered when LLM streaming completes
type StreamCompletedEvent struct {
	Message   sdk.Message
	ToolCalls []*sdk.ChatCompletionMessageToolCall
	Reasoning string
}

func (e StreamCompletedEvent) EventType() string { return "StreamCompleted" }

// ToolsCompletedEvent is triggered when all tools finish executing.
type ToolsCompletedEvent struct{}

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

// ApprovalDecision is a pre-fetched approval verdict for one tool call. Reason
// is non-empty only for judge rejections, matching RequestToolApproval.
type ApprovalDecision struct {
	Approved bool
	Reason   string
}

// StateContext gives state handlers the agent's shared state and callbacks.
// MaxConcurrentTools bounds approved tools running while later ones await
// approval. RequestJudgeApprovals decides a round's judge calls in one request,
// AttachToolImages hands tool images to the model, PresentPlan reports a
// presented plan, and WaitForBackgroundTasks runs only for non-chat runs.
type StateContext struct {
	StateMachine AgentStateMachine
	AgentCtx     *AgentContext

	Events chan AgentEvent

	WaitGroup *sync.WaitGroup
	Mutex     *sync.Mutex

	CurrentMessage   *sdk.Message
	CurrentToolCalls *[]*sdk.ChatCompletionMessageToolCall
	CurrentReasoning *string

	Tools                agentdomain.ToolManifestLookup
	ToolsNeedingApproval *[]sdk.ChatCompletionMessageToolCall
	CurrentToolIndex     *int
	ToolResults          *[]convdomain.ConversationEntry

	Request *agentdomain.AgentRequest

	MaxConcurrentTools int

	ToolExecutor   *func()
	StartStreaming func()

	GetMetrics            func(requestID string) *agentdomain.ChatMetrics
	ShouldRequireApproval func(toolCall *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool
	ApprovalDelivery      func(toolCall *sdk.ChatCompletionMessageToolCall) string
	BlockedToolHint       func(toolCall *sdk.ChatCompletionMessageToolCall) string
	AddMessage            func(entry convdomain.ConversationEntry) error
	BatchDrainQueue       func() int
	RequestToolApproval   func(toolCall sdk.ChatCompletionMessageToolCall) (bool, string, error)
	RequestJudgeApprovals func(calls []sdk.ChatCompletionMessageToolCall) map[string]ApprovalDecision
	ExecuteToolInternal   func(toolCall sdk.ChatCompletionMessageToolCall, isApproved bool) convdomain.ConversationEntry
	ForgetPendingToolCall func(toolCallID string)
	GetAgentMode          func() agentdomain.AgentMode
	PublishChatEvent      func(event agentdomain.ChatEvent)
	PublishChatComplete   func(reasoning string, toolCalls []sdk.ChatCompletionMessageToolCall, metrics *agentdomain.ChatMetrics)
	PublishChatCancelled  func(metrics *agentdomain.ChatMetrics)
	PublishToolResults    func(results []convdomain.ConversationEntry)
	AttachToolImages      func(results []convdomain.ConversationEntry)
	PresentPlan           func(results []convdomain.ConversationEntry) bool

	DispatchHooks func(hook agentdomain.HookPoint)

	WaitForBackgroundTasks func()
}
