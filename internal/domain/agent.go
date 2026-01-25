package domain

import (
	"context"
	"time"

	adk "github.com/inference-gateway/adk/types"
	sdk "github.com/inference-gateway/sdk"
)

// SDKClient is our interface wrapper for the SDK client to make it testable
type SDKClient interface {
	WithOptions(opts *sdk.CreateChatCompletionRequest) SDKClient
	WithMiddlewareOptions(opts *sdk.MiddlewareOptions) SDKClient
	WithTools(tools *[]sdk.ChatCompletionTool) SDKClient
	GenerateContent(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error)
	GenerateContentStream(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (<-chan sdk.SSEvent, error)
}

// AgentContext represents the execution context for the agent state machine
type AgentContext struct {
	RequestID        string
	Conversation     *[]sdk.Message
	MessageQueue     MessageQueue
	ConversationRepo ConversationRepository
	ToolCalls        []*sdk.ChatCompletionMessageToolCall
	Turns            int
	MaxTurns         int
	HasToolResults   bool
	ApprovalPolicy   ApprovalPolicy
	Ctx              context.Context
	IsChatMode       bool
}

// StateGuard is a function that determines if a state transition should occur
type StateGuard func(ctx *AgentContext) bool

// StateAction is a function executed on state transitions
type StateAction func(ctx *AgentContext) error

// AgentStateMachine manages agent execution state transitions
type AgentStateMachine interface {
	// Transition attempts to transition to the target state
	Transition(ctx *AgentContext, targetState AgentExecutionState) error

	// GetCurrentState returns the current state (thread-safe)
	GetCurrentState() AgentExecutionState

	// GetPreviousState returns the previous state (thread-safe)
	GetPreviousState() AgentExecutionState

	// CanTransition checks if a transition is valid without executing it
	CanTransition(ctx *AgentContext, targetState AgentExecutionState) bool

	// GetValidTransitions returns all valid transitions from current state
	GetValidTransitions(ctx *AgentContext) []AgentExecutionState

	// Reset resets the state machine to idle
	Reset()
}

// AgentRequest represents a request to the agent service
type AgentRequest struct {
	RequestID  string        `json:"request_id"`
	Model      string        `json:"model"`
	Messages   []sdk.Message `json:"messages"`
	IsChatMode bool          `json:"is_chat_mode"`
}

// AgentService handles agent operations with both sync and streaming modes
type AgentService interface {
	// Run executes an agent task synchronously (for background/batch processing)
	Run(ctx context.Context, req *AgentRequest) (*ChatSyncResponse, error)

	// RunWithStream executes an agent task with streaming (for interactive chat)
	RunWithStream(ctx context.Context, req *AgentRequest) (<-chan ChatEvent, error)

	// CancelRequest cancels an active request
	CancelRequest(requestID string) error

	// GetMetrics returns metrics for a completed request
	GetMetrics(requestID string) *ChatMetrics
}

// CachedAgentCard represents a cached agent card with metadata
type CachedAgentCard struct {
	Card      *adk.AgentCard `json:"card"`
	URL       string         `json:"url"`
	FetchedAt time.Time      `json:"fetched_at"`
}

// A2AAgentService manages A2A agent operations
type A2AAgentService interface {
	GetAgentCards(ctx context.Context) ([]*CachedAgentCard, error)
	GetConfiguredAgents() []string
}

// AgentManager manages the lifecycle of A2A agent containers
type AgentManager interface {
	// StartAgents starts all agents configured with run: true
	StartAgents(ctx context.Context) error

	// StopAgents stops all running agent containers
	StopAgents(ctx context.Context) error

	// StopAgent stops a specific agent container by name
	StopAgent(ctx context.Context, agentName string) error

	// IsRunning returns whether any agents are running
	IsRunning() bool

	// SetStatusCallback sets the callback function for agent status updates
	SetStatusCallback(callback func(agentName string, state AgentState, message string, url string, image string))
}
