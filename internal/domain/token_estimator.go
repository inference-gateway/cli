package domain

import sdk "github.com/inference-gateway/sdk"

// TokenEstimator provides token count estimation for LLM content
type TokenEstimator interface {
	// GetToolStats returns token count and tool count for a given agent mode
	GetToolStats(toolService ToolService, agentMode AgentMode) (tokens int, count int)

	// EstimateMessagesTokens estimates the total tokens for a slice of messages
	EstimateMessagesTokens(messages []sdk.Message) int
}
