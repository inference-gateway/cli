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

// AgentRequest represents a request to the agent service
type AgentRequest struct {
	RequestID string        `json:"request_id"`
	Model     string        `json:"model"`
	Messages  []sdk.Message `json:"messages"`
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
	GetAgentCard(ctx context.Context, agentURL string) (*adk.AgentCard, error)
	GetConfiguredAgents() []string
	GetAllAgentCards(ctx context.Context) ([]*CachedAgentCard, error)
	GetSystemPromptAgentInfo(ctx context.Context) string
}
