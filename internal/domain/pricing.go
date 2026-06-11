package domain

import "strings"

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
	PerModelStats   map[string]*ModelCostStats
	Currency        string
}

// PricingService provides pricing information and cost calculation for different models.
// Note: This interface returns float64 for pricing to avoid import cycles.
// The actual ModelPricing struct is defined in the config package.
type PricingService interface {
	// IsEnabled returns whether pricing is enabled in the configuration.
	IsEnabled() bool

	// GetInputPrice retrieves the input price per million tokens for a specific model.
	// Returns 0.0 for unknown models (e.g., Ollama, custom models).
	GetInputPrice(model string) float64

	// GetOutputPrice retrieves the output price per million tokens for a specific model.
	// Returns 0.0 for unknown models (e.g., Ollama, custom models).
	GetOutputPrice(model string) float64

	// CalculateCost computes the total cost for a given number of input and output tokens.
	CalculateCost(model string, inputTokens, outputTokens int) (inputCost, outputCost, totalCost float64)

	// RequiresPro reports whether the model is gated behind a paid Pro
	// subscription (e.g. some Ollama Cloud models). Resolves custom prices
	// first, then defaults. Returns false when pricing is disabled or the
	// model has no entry.
	RequiresPro(model string) bool

	// FormatModelPricing returns a formatted string describing the model's pricing.
	// Returns empty string if pricing is disabled or the model has no pricing entry.
	// Returns "free" only when an explicit pricing entry sets both prices to 0.0.
	// Returns "$X.XX/$Y.YY per MTok" for paid models.
	FormatModelPricing(model string) string
}

// FormatModelPricingLabel builds a human-readable pricing/availability label for
// a model, combining the per-token price with a subscription marker. A
// subscription model has no per-token price ($0/$0 → "free"), which would be
// misleading, so the bare "free" token is replaced by "subscription". A model
// that is both priced and subscription-gated keeps its price and gains the
// marker. Returns "" when there is nothing to show (pricing disabled, no entry,
// and not subscription-gated).
func FormatModelPricingLabel(pricingService PricingService, model string) string {
	if pricingService == nil {
		return ""
	}

	parts := make([]string, 0, 2)
	pricing := pricingService.FormatModelPricing(model)
	requiresPro := pricingService.RequiresPro(model)

	if pricing != "" && (!requiresPro || pricing != "free") {
		parts = append(parts, pricing)
	}
	if requiresPro {
		parts = append(parts, "subscription")
	}

	return strings.Join(parts, ", ")
}
