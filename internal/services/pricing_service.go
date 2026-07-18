package services

import (
	"fmt"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// freeLocalProviders are providers that serve locally-hosted open-weight
// models. Their model names are arbitrary (whatever the user loaded), so
// instead of enumerating per-model zero-price entries, any model under these
// prefixes resolves to free. ollama_cloud is NOT here - it has paid
// RequiresPro entries.
var freeLocalProviders = map[string]bool{
	"llamacpp": true,
	"ollama":   true,
}

// PricingServiceImpl implements the PricingService interface.
type PricingServiceImpl struct {
	config        *config.PricingConfig
	defaultPrices map[string]config.ModelPricing
}

// NewPricingService creates a new pricing service instance.
func NewPricingService(cfg *config.PricingConfig) domain.PricingService {
	return &PricingServiceImpl{
		config:        cfg,
		defaultPrices: config.DefaultModelPricing,
	}
}

// IsEnabled returns whether pricing is enabled in the configuration.
func (p *PricingServiceImpl) IsEnabled() bool {
	return p.config.Enabled
}

// resolvePricing returns the input/output price for a model and whether it's known.
// Custom prices take precedence over defaults.
func (p *PricingServiceImpl) resolvePricing(model string) (input, output float64, ok bool) {
	if customPrice, exists := p.config.CustomPrices[model]; exists {
		return customPrice.InputPricePerMToken, customPrice.OutputPricePerMToken, true
	}
	if defaultPrice, exists := p.defaultPrices[model]; exists {
		return defaultPrice.InputPricePerMToken, defaultPrice.OutputPricePerMToken, true
	}
	if provider, _, found := strings.Cut(model, "/"); found && freeLocalProviders[provider] {
		return 0.0, 0.0, true
	}
	return 0.0, 0.0, false
}

// resolveRequiresPro returns whether a model is gated behind a Pro subscription.
// Custom prices take precedence over defaults, matching resolvePricing.
func (p *PricingServiceImpl) resolveRequiresPro(model string) bool {
	if customPrice, exists := p.config.CustomPrices[model]; exists {
		return customPrice.RequiresPro
	}
	if defaultPrice, exists := p.defaultPrices[model]; exists {
		return defaultPrice.RequiresPro
	}
	return false
}

// RequiresPro reports whether the model is gated behind a paid Pro subscription.
// Returns false when pricing is disabled or the model has no entry.
func (p *PricingServiceImpl) RequiresPro(model string) bool {
	if !p.config.Enabled {
		return false
	}
	return p.resolveRequiresPro(model)
}

// GetInputPrice retrieves the input price per million tokens for a specific model.
// Returns 0.0 for unknown models (e.g., Ollama, custom models).
func (p *PricingServiceImpl) GetInputPrice(model string) float64 {
	if !p.config.Enabled {
		return 0.0
	}
	input, _, _ := p.resolvePricing(model)
	return input
}

// GetOutputPrice retrieves the output price per million tokens for a specific model.
// Returns 0.0 for unknown models (e.g., Ollama, custom models).
func (p *PricingServiceImpl) GetOutputPrice(model string) float64 {
	if !p.config.Enabled {
		return 0.0
	}
	_, output, _ := p.resolvePricing(model)
	return output
}

// CalculateCost computes the total cost for a given number of input and output tokens.
// Returns inputCost, outputCost, and totalCost in USD (or configured currency).
func (p *PricingServiceImpl) CalculateCost(model string, inputTokens, outputTokens int) (inputCost, outputCost, totalCost float64) {
	if !p.config.Enabled {
		return 0.0, 0.0, 0.0
	}

	inputPrice := p.GetInputPrice(model)
	outputPrice := p.GetOutputPrice(model)

	inputCost = (float64(inputTokens) / 1_000_000.0) * inputPrice
	outputCost = (float64(outputTokens) / 1_000_000.0) * outputPrice
	totalCost = inputCost + outputCost

	return inputCost, outputCost, totalCost
}

// FormatModelPricing returns a formatted string describing the model's pricing.
// Returns empty string if pricing is disabled or the model has no pricing entry
// (callers should not assume "no entry" means "free").
// Returns "free" only when an explicit pricing entry sets both prices to 0.0.
// Returns "$X.XX/$Y.YY per MTok" for paid models.
func (p *PricingServiceImpl) FormatModelPricing(model string) string {
	if !p.config.Enabled {
		return ""
	}

	inputPrice, outputPrice, ok := p.resolvePricing(model)
	if !ok {
		return ""
	}

	if inputPrice == 0.0 && outputPrice == 0.0 {
		return "free"
	}

	return fmt.Sprintf("$%.2f/$%.2f per MTok", inputPrice, outputPrice)
}
