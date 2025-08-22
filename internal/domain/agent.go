package domain

import (
	"context"

	sdk "github.com/inference-gateway/sdk"
)

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
