package domain

// ModelCostStats tracks cost statistics for a specific model within a session.
// This allows detailed breakdown when multiple models are used in the same conversation.
type ModelCostStats struct {
	Model        string
	InputTokens  int
	OutputTokens int
	InputCost    float64
	OutputCost   float64
	TotalCost    float64
	RequestCount int
}

// SessionCostStats aggregates cost information for an entire session.
// It provides both total costs and per-model breakdowns.
type SessionCostStats struct {
	TotalCost       float64
	TotalInputCost  float64
	TotalOutputCost float64
	PerModelStats   map[string]*ModelCostStats // keyed by model name
	Currency        string
}

// PricingService provides pricing information and cost calculation for different models.
// Note: This interface returns float64 for pricing to avoid import cycles.
// The actual ModelPricing struct is defined in the config package.
type PricingService interface {
	// GetInputPrice retrieves the input price per million tokens for a specific model.
	// Returns 0.0 for unknown models (e.g., Ollama, custom models).
	GetInputPrice(model string) float64

	// GetOutputPrice retrieves the output price per million tokens for a specific model.
	// Returns 0.0 for unknown models (e.g., Ollama, custom models).
	GetOutputPrice(model string) float64

	// CalculateCost computes the total cost for a given number of input and output tokens.
	CalculateCost(model string, inputTokens, outputTokens int) (inputCost, outputCost, totalCost float64)
}
