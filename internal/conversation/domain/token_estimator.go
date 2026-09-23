package domain

import (
	"context"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TokenEstimator provides token count estimation for LLM content
type TokenEstimator interface {
	// GetToolStats returns token count and tool count for a given agent mode
	GetToolStats(toolService agentdomain.ToolService, agentMode agentdomain.AgentMode) (tokens int, count int)

	// EstimateMessagesTokens estimates the total tokens for a slice of messages
	EstimateMessagesTokens(messages []sdk.Message) int

	// EffectiveContextTokens estimates what the *next* request will carry: the
	// larger of the gateway-reported last-request size and a fresh estimate of
	// the current buffer. The max catches a single-turn tool-output spike that a
	// stale lastInputTokens alone would miss.
	EffectiveContextTokens(lastInputTokens int, messages []sdk.Message) int
}

// SessionRollover continues a conversation in a fresh session (summary
// preserved) once it crosses the compact threshold, keeping a stable group key
// across the chain.
type SessionRollover interface {
	// ResolveSessionID maps a raw (possibly group) id to the group's current
	// session id; returns (sessionID, groupKey, error).
	ResolveSessionID(rawID string) (string, string, error)
	ShouldRollover(model string) bool
	// MaybeRollover rolls over when ShouldRollover fires and returns the new
	// session id and true; failures are logged and reported as no rollover.
	MaybeRollover(ctx context.Context, model, groupKey string) (string, bool)
	PerformRollover(ctx context.Context, model, groupKey string) (string, error)
}
