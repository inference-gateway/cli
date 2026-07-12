package domain

import sdk "github.com/inference-gateway/sdk"

// TokenEstimator provides token count estimation for LLM content
type TokenEstimator interface {
	// GetToolStats returns token count and tool count for a given agent mode
	GetToolStats(toolService ToolService, agentMode AgentMode) (tokens int, count int)

	// EstimateMessagesTokens estimates the total tokens for a slice of messages
	EstimateMessagesTokens(messages []sdk.Message) int

	// EffectiveContextTokens estimates what the *next* request will carry: the
	// larger of the gateway-reported last-request size and a fresh estimate of
	// the current buffer. The max catches a single-turn tool-output spike that a
	// stale lastInputTokens alone would miss.
	EffectiveContextTokens(lastInputTokens int, messages []sdk.Message) int
}
