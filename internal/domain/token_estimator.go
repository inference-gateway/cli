package domain

// TokenEstimator provides token count estimation for LLM content
type TokenEstimator interface {
	// GetToolStats returns token count and tool count for a given agent mode
	GetToolStats(toolService ToolService, agentMode AgentMode) (tokens int, count int)
}
