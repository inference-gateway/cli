package services

import (
	"fmt"
	"strconv"
	"sync"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// gatewayPrice is a per-model price from /v1/models?include=pricing,
// converted from per-token decimal strings to per-MTok floats at ingest.
type gatewayPrice struct {
	inputPerMTok     float64
	outputPerMTok    float64
	cacheReadPerMTok *float64
}

var (
	gatewayPricesMu sync.RWMutex
	gatewayPrices   map[string]gatewayPrice
)

// setGatewayPricing replaces the gateway-reported model prices. Keys are
// full "provider/model" ids, matched exactly like CustomPrices/defaults.
func setGatewayPricing(prices map[string]gatewayPrice) {
	gatewayPricesMu.Lock()
	gatewayPrices = prices
	gatewayPricesMu.Unlock()
}

func gatewayPriceFor(model string) (gatewayPrice, bool) {
	gatewayPricesMu.RLock()
	defer gatewayPricesMu.RUnlock()
	price, ok := gatewayPrices[model]
	return price, ok
}

// parseGatewayPricing converts an sdk.Pricing (per-token decimal strings)
// into a gatewayPrice. Returns false on missing or unparseable prices.
func parseGatewayPricing(p *sdk.Pricing) (gatewayPrice, bool) {
	if p == nil {
		return gatewayPrice{}, false
	}
	input, errIn := strconv.ParseFloat(p.InputPerToken, 64)
	output, errOut := strconv.ParseFloat(p.OutputPerToken, 64)
	if errIn != nil || errOut != nil {
		return gatewayPrice{}, false
	}
	price := gatewayPrice{inputPerMTok: input * 1e6, outputPerMTok: output * 1e6}
	if p.CacheReadPerToken != nil {
		if cacheRead, err := strconv.ParseFloat(*p.CacheReadPerToken, 64); err == nil {
			perMTok := cacheRead * 1e6
			price.cacheReadPerMTok = &perMTok
		}
	}
	return price, true
}

// knownProModels is the curated set of model IDs that require a Pro subscription.
// These models have no per-token price ($0/$0) but are not freely available —
// they are gated server-side. The gateway does not report RequiresPro in its
// pricing metadata, so this small set is kept here instead.
// ponytail: add entries here as new Pro-gated models appear. This map is ~4
// lines, not the 910-line DefaultModelPricing table we deleted.
var knownProModels = map[string]bool{
	"ollama_cloud/deepseek-v4-pro":   true,
	"ollama_cloud/deepseek-v4-flash": true,
}

// PricingServiceImpl implements the PricingService interface.
type PricingServiceImpl struct {
	config *config.PricingConfig
}

// NewPricingService creates a new pricing service instance.
func NewPricingService(cfg *config.PricingConfig) domain.PricingService {
	return &PricingServiceImpl{
		config: cfg,
	}
}

// IsEnabled returns whether pricing is enabled in the configuration.
func (p *PricingServiceImpl) IsEnabled() bool {
	return p.config.Enabled
}

// resolvePricing returns the input/output price for a model and whether it's known.
// Custom prices win, then gateway-reported prices; anything else is unknown.
// cacheRead is per-MTok when the gateway reports a cache-read rate, nil otherwise.
func (p *PricingServiceImpl) resolvePricing(model string) (input, output float64, cacheRead *float64, ok bool) {
	if customPrice, exists := p.config.CustomPrices[model]; exists {
		return customPrice.InputPricePerMToken, customPrice.OutputPricePerMToken, nil, true
	}
	if price, exists := gatewayPriceFor(model); exists {
		return price.inputPerMTok, price.outputPerMTok, price.cacheReadPerMTok, true
	}
	return 0.0, 0.0, nil, false
}

// resolveRequiresPro returns whether a model is gated behind a Pro subscription.
// Custom prices take precedence, then the known-Pro set (curated, not exhaustive),
// matching the precedent of resolvePricing.
func (p *PricingServiceImpl) resolveRequiresPro(model string) bool {
	if customPrice, exists := p.config.CustomPrices[model]; exists {
		return customPrice.RequiresPro
	}
	if knownProModels[model] {
		return true
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
	input, _, _, _ := p.resolvePricing(model)
	return input
}

// GetOutputPrice retrieves the output price per million tokens for a specific model.
// Returns 0.0 for unknown models (e.g., Ollama, custom models).
func (p *PricingServiceImpl) GetOutputPrice(model string) float64 {
	if !p.config.Enabled {
		return 0.0
	}
	_, output, _, _ := p.resolvePricing(model)
	return output
}

// CalculateCost computes the total cost for a given number of input, output
// and cached-prompt tokens. cachedTokens is the cached subset of inputTokens;
// it is billed at the gateway's cache-read rate when known, otherwise at the
// full input rate. Returns inputCost, outputCost, and totalCost in USD (or
// configured currency).
func (p *PricingServiceImpl) CalculateCost(model string, inputTokens, outputTokens, cachedTokens int) (inputCost, outputCost, totalCost float64) {
	if !p.config.Enabled {
		return 0.0, 0.0, 0.0
	}

	inputPrice, outputPrice, cacheReadPrice, _ := p.resolvePricing(model)

	cached := min(max(cachedTokens, 0), inputTokens)
	cacheRate := inputPrice
	if cacheReadPrice != nil {
		cacheRate = *cacheReadPrice
	}

	inputCost = (float64(inputTokens-cached)*inputPrice + float64(cached)*cacheRate) / 1_000_000.0
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

	inputPrice, outputPrice, _, ok := p.resolvePricing(model)
	if !ok {
		return ""
	}

	if inputPrice == 0.0 && outputPrice == 0.0 {
		return "free"
	}

	return fmt.Sprintf("$%.2f/$%.2f per MTok", inputPrice, outputPrice)
}
