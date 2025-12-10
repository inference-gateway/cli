package services

import (
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

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

// GetInputPrice retrieves the input price per million tokens for a specific model.
// Returns 0.0 for unknown models (e.g., Ollama, custom models).
func (p *PricingServiceImpl) GetInputPrice(model string) float64 {
	if !p.config.Enabled {
		return 0.0
	}

	if customPrice, exists := p.config.CustomPrices[model]; exists {
		return customPrice.InputPricePerMToken
	}

	if defaultPrice, exists := p.defaultPrices[model]; exists {
		return defaultPrice.InputPricePerMToken
	}

	return 0.0
}

// GetOutputPrice retrieves the output price per million tokens for a specific model.
// Returns 0.0 for unknown models (e.g., Ollama, custom models).
func (p *PricingServiceImpl) GetOutputPrice(model string) float64 {
	if !p.config.Enabled {
		return 0.0
	}

	if customPrice, exists := p.config.CustomPrices[model]; exists {
		return customPrice.OutputPricePerMToken
	}

	if defaultPrice, exists := p.defaultPrices[model]; exists {
		return defaultPrice.OutputPricePerMToken
	}

	return 0.0
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
