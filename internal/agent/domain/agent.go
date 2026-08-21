package domain

import (
	"context"
	"errors"

	sdk "github.com/inference-gateway/sdk"
)

// ErrMaxTurnsReached is returned when the agent reaches its maximum turn limit
// without completing the task. Callers should use errors.Is to check for it.
var ErrMaxTurnsReached = errors.New("max_turns_reached")

// AgentRequest represents a request to the agent service
type AgentRequest struct {
	RequestID              string        `json:"request_id"`
	Model                  string        `json:"model"`
	Messages               []sdk.Message `json:"messages"`
	IsChatMode             bool          `json:"is_chat_mode"`
	ApprovalBrokerAttached bool          `json:"approval_broker_attached"`
	GroupKey               string        `json:"group_key,omitempty"`
}

// AgentService handles agent operations with both sync and streaming modes
type AgentService interface {
	// Run executes an agent task synchronously (for background/batch processing)
	Run(ctx context.Context, req *AgentRequest) (*ChatSyncResponse, error)

	// RunWithStream executes an agent task with streaming (for interactive chat)
	RunWithStream(ctx context.Context, req *AgentRequest) (<-chan ChatEvent, error)

	// RunStreaming executes a single model turn with streaming, invoking onDelta
	// for each content/reasoning/tool-call delta, and returns the assembled
	// response like Run. For callers that own their own agentic loop (the
	// headless AG-UI agent) but want token-level output. onDelta may be nil.
	RunStreaming(ctx context.Context, req *AgentRequest, onDelta func(content, reasoning string, toolCalls []sdk.ChatCompletionMessageToolCallChunk)) (*ChatSyncResponse, error)

	// CancelRequest cancels an active request
	CancelRequest(requestID string) error

	// GetMetrics returns metrics for a completed request
	GetMetrics(requestID string) *ChatMetrics

	// BuildSystemPrompt returns the static system prompt sent as message[0],
	// byte-stable across turns; volatile context travels separately as a hidden
	// per-request message (see `infer debug agent system_prompt`).
	BuildSystemPrompt() string

	// SetReasoningEffort updates the reasoning effort applied to subsequent
	// requests. An empty string resets to the provider default.
	SetReasoningEffort(effort string) error

	// GetReasoningEffort returns the effort level currently applied to
	// requests ("" = provider default).
	GetReasoningEffort() string
}

// AgentManager manages the lifecycle of A2A agent containers
type AgentManager interface {
	// StartAgents starts all agents configured with run: true
	StartAgents(ctx context.Context) error

	// WaitForAgentsReady blocks until every run:true agent started by
	// StartAgents has settled (ready or failed), or ctx is done
	WaitForAgentsReady(ctx context.Context)

	// StopAgents stops all running agent containers
	StopAgents(ctx context.Context) error

	// StopAgent stops a specific agent container by name
	StopAgent(ctx context.Context, agentName string) error

	// IsRunning returns whether any agents are running
	IsRunning() bool

	// SetStatusCallback sets the callback function for agent status updates
	SetStatusCallback(callback func(agentName string, state AgentState, message string, url string, image string))

	// SetPullProgressCallback sets the callback function for image pull progress updates
	SetPullProgressCallback(callback func(agentName string, done, total int))
}
