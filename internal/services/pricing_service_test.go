package services

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestPricingService_FormatModelPricing(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		model          string
		customPrices   map[string]config.CustomPricing
		expectedOutput string
	}{
		{
			name:           "pricing disabled returns empty string",
			enabled:        false,
			model:          "gpt-4",
			expectedOutput: "",
		},
		{
			name:    "free model returns 'free'",
			enabled: true,
			model:   "free-model",
			customPrices: map[string]config.CustomPricing{
				"free-model": {
					InputPricePerMToken:  0.0,
					OutputPricePerMToken: 0.0,
				},
			},
			expectedOutput: "free",
		},
		{
			name:    "paid model returns formatted pricing",
			enabled: true,
			model:   "claude-opus-4",
			customPrices: map[string]config.CustomPricing{
				"claude-opus-4": {
					InputPricePerMToken:  15.00,
					OutputPricePerMToken: 75.00,
				},
			},
			expectedOutput: "$15.00/$75.00 per MTok",
		},
		{
			name:    "model with fractional pricing",
			enabled: true,
			model:   "deepseek-v4-flash",
			customPrices: map[string]config.CustomPricing{
				"deepseek-v4-flash": {
					InputPricePerMToken:  0.14,
					OutputPricePerMToken: 0.28,
				},
			},
			expectedOutput: "$0.14/$0.28 per MTok",
		},
		{
			name:           "unknown model returns empty string",
			enabled:        true,
			model:          "unknown-model",
			customPrices:   map[string]config.CustomPricing{},
			expectedOutput: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.PricingConfig{
				Enabled:      tt.enabled,
				CustomPrices: tt.customPrices,
			}

			service := NewPricingService(cfg)
			result := service.FormatModelPricing(tt.model)

			assert.Equal(t, tt.expectedOutput, result)
		})
	}
}

func TestPricingService_GoogleModelDefaults(t *testing.T) {
	tests := []struct {
		model          string
		expectedOutput string
	}{
		{"google/models/gemini-2.5-pro", ""},
		{"google/models/gemini-2.5-flash", ""},
		{"google/models/gemini-2.5-flash-lite", ""},
		{"google/models/gemini-2.0-flash", ""},
		{"google/models/gemini-2.0-flash-001", ""},
		{"google/models/gemini-2.0-flash-lite", ""},
		{"google/models/gemini-3-flash-preview", ""},
		{"google/models/gemini-3.1-pro-preview", ""},
		{"google/models/gemma-3-1b-it", ""},
		{"google/models/gemma-3-27b-it", ""},
		{"google/models/gemma-3n-e2b-it", ""},
		{"google/models/gemma-4-31b-it", ""},
		{"google/models/imagen-4.0-generate-001", ""},
		{"google/models/veo-3.0-generate-001", ""},
	}

	cfg := &config.PricingConfig{
		Enabled:      true,
		CustomPrices: map[string]config.CustomPricing{},
	}
	service := NewPricingService(cfg)

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := service.FormatModelPricing(tt.model)
			assert.Equal(t, tt.expectedOutput, result)
		})
	}
}

func TestPricingService_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "pricing enabled",
			enabled:  true,
			expected: true,
		},
		{
			name:     "pricing disabled",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.PricingConfig{
				Enabled: tt.enabled,
			}

			service := NewPricingService(cfg)
			result := service.IsEnabled()

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPricingService_RequiresPro(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		model        string
		customPrices map[string]config.CustomPricing
		expected     bool
	}{
		{
			name:     "pricing disabled returns false even for unknown model",
			enabled:  false,
			model:    "ollama_cloud/deepseek-v4-pro",
			expected: false,
		},
		{
			name:    "custom entry marked pro returns true",
			enabled: true,
			model:   "custom-model",
			customPrices: map[string]config.CustomPricing{
				"custom-model": {RequiresPro: true},
			},
			expected: true,
		},
		{
			name:    "custom entry overrides default pro to false",
			enabled: true,
			model:   "ollama_cloud/deepseek-v4-pro",
			customPrices: map[string]config.CustomPricing{
				"ollama_cloud/deepseek-v4-pro": {
					InputPricePerMToken:  0.0,
					OutputPricePerMToken: 0.0,
					RequiresPro:          false,
				},
			},
			expected: false,
		},
		{
			name:     "known pro model from curated set returns true",
			enabled:  true,
			model:    "ollama_cloud/deepseek-v4-pro",
			expected: true,
		},
		{
			name:     "known flash pro model from curated set returns true",
			enabled:  true,
			model:    "ollama_cloud/deepseek-v4-flash",
			expected: true,
		},
		{
			name:     "previously-default free ollama cloud model returns false",
			enabled:  true,
			model:    "ollama_cloud/kimi-k2.5",
			expected: false,
		},
		{
			name:     "unknown model returns false",
			enabled:  true,
			model:    "unknown-model",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.PricingConfig{
				Enabled:      tt.enabled,
				CustomPrices: tt.customPrices,
			}

			service := NewPricingService(cfg)
			result := service.RequiresPro(tt.model)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPricingService_MinimaxDefaults(t *testing.T) {
	// With the static DefaultModelPricing table removed, native MiniMax
	// providers resolve as unknown without gateway-reported pricing.
	service := NewPricingService(&config.PricingConfig{Enabled: true})

	models := []string{
		"minimax/MiniMax-M2",
		"minimax/MiniMax-M2.1",
		"minimax/MiniMax-M2.5",
		"minimax/MiniMax-M2.7",
		"minimax/MiniMax-M3",
	}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			assert.InDelta(t, 0.0, service.GetInputPrice(model), 1e-9)
			assert.InDelta(t, 0.0, service.GetOutputPrice(model), 1e-9)
			assert.Equal(t, "", service.FormatModelPricing(model))
			assert.False(t, service.RequiresPro(model), "native MiniMax models are unknown without gateway data")
		})
	}
}

// TestFormatModelPricingLabel exercises the shared label helper that the model
// view and autocomplete both use: it must suppress the misleading "free" token
// for subscription models and keep the price for paid+subscription models.
func TestFormatModelPricingLabel(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		model        string
		customPrices map[string]config.CustomPricing
		expected     string
	}{
		{
			name:     "known subscription model shows subscription",
			enabled:  true,
			model:    "ollama_cloud/deepseek-v4-pro",
			expected: "subscription",
		},
		{
			name:    "free non-pro model shows free",
			enabled: true,
			model:   "free-model",
			customPrices: map[string]config.CustomPricing{
				"free-model": {InputPricePerMToken: 0.0, OutputPricePerMToken: 0.0},
			},
			expected: "free",
		},
		{
			name:    "paid model shows price",
			enabled: true,
			model:   "paid-model",
			customPrices: map[string]config.CustomPricing{
				"paid-model": {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0},
			},
			expected: "$3.00/$15.00 per MTok",
		},
		{
			name:    "paid and subscription model shows both",
			enabled: true,
			model:   "paid-subscription-model",
			customPrices: map[string]config.CustomPricing{
				"paid-subscription-model": {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, RequiresPro: true},
			},
			expected: "$3.00/$15.00 per MTok, subscription",
		},
		{
			name:     "unknown model shows nothing",
			enabled:  true,
			model:    "unknown-model",
			expected: "",
		},
		{
			name:     "pricing disabled shows nothing",
			enabled:  false,
			model:    "ollama_cloud/deepseek-v4-pro",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.PricingConfig{
				Enabled:      tt.enabled,
				CustomPrices: tt.customPrices,
			}

			service := NewPricingService(cfg)
			result := domain.FormatModelPricingLabel(service, tt.model)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestPricingService_FreeLocalProviders verifies that locally-hosted providers
// (llamacpp, ollama) resolve to "free" for ANY model name - their model list is
// whatever the user loaded, so there are no per-model default entries. The
// ollama prefix must NOT capture ollama_cloud, which has its own paid catalog.
func TestPricingService_FreeLocalProviders(t *testing.T) {
	service := NewPricingService(&config.PricingConfig{Enabled: true})

	for _, model := range []string{
		"llamacpp/llama", "llamacpp/my-custom-quant.gguf",
		"ollama/llama3.2", "ollama/anything-at-all",
	} {
		t.Run(model, func(t *testing.T) {
			assert.Equal(t, "free", service.FormatModelPricing(model))
			_, _, total := service.CalculateCost(model, 100000, 50000, 0)
			assert.Zero(t, total)
			assert.False(t, service.RequiresPro(model))
		})
	}

	assert.Empty(t, service.FormatModelPricing("ollama_cloud/brand-new-model"),
		"ollama_cloud must not be captured by the ollama free-provider prefix")
	assert.Empty(t, service.FormatModelPricing("no-provider-prefix"))

	custom := NewPricingService(&config.PricingConfig{
		Enabled: true,
		CustomPrices: map[string]config.CustomPricing{
			"llamacpp/hosted": {InputPricePerMToken: 1.0, OutputPricePerMToken: 2.0},
		},
	})
	assert.Equal(t, "$1.00/$2.00 per MTok", custom.FormatModelPricing("llamacpp/hosted"),
		"custom prices must win over the free-provider fallback")
}

// TestPricingService_GatewayPricing covers the /v1/models?include=pricing
// tier: gateway prices work for known models, config custom prices still win.
func TestPricingService_GatewayPricing(t *testing.T) {
	cacheRead := 0.25
	setGatewayPricing(map[string]gatewayPrice{
		"moonshot/kimi-k3": {inputPerMTok: 2.0, outputPerMTok: 8.0},
		"openai/gpt-4o":    {inputPerMTok: 2.5, outputPerMTok: 10.0, cacheReadPerMTok: &cacheRead},
	})
	defer setGatewayPricing(nil)

	service := NewPricingService(&config.PricingConfig{Enabled: true})

	assert.Equal(t, 2.0, service.GetInputPrice("moonshot/kimi-k3"),
		"gateway price must be returned for known model")
	assert.Equal(t, 8.0, service.GetOutputPrice("moonshot/kimi-k3"))
	assert.Equal(t, "$2.50/$10.00 per MTok", service.FormatModelPricing("openai/gpt-4o"))

	custom := NewPricingService(&config.PricingConfig{
		Enabled: true,
		CustomPrices: map[string]config.CustomPricing{
			"openai/gpt-4o": {InputPricePerMToken: 1.0, OutputPricePerMToken: 2.0},
		},
	})
	assert.Equal(t, 1.0, custom.GetInputPrice("openai/gpt-4o"),
		"config custom prices must win over gateway data")
}

// TestPricingService_CalculateCost_CachedTokens covers the cache-read
// discount: cached tokens bill at the gateway cache-read rate when known,
// at the full input rate otherwise, and the cached count is clamped to
// [0, inputTokens].
func TestPricingService_CalculateCost_CachedTokens(t *testing.T) {
	cacheRead := 0.25
	setGatewayPricing(map[string]gatewayPrice{
		"g/discounted": {inputPerMTok: 2.5, outputPerMTok: 10.0, cacheReadPerMTok: &cacheRead},
		"g/no-rate":    {inputPerMTok: 2.5, outputPerMTok: 10.0},
	})
	defer setGatewayPricing(nil)

	service := NewPricingService(&config.PricingConfig{Enabled: true})

	tests := []struct {
		name                  string
		model                 string
		input, output, cached int
		wantInput, wantTotal  float64
	}{
		{"no cached tokens", "g/discounted", 1_000_000, 100_000, 0, 2.5, 3.5},
		{"cached at cache-read rate", "g/discounted", 1_000_000, 100_000, 400_000, 1.6, 2.6},
		{"nil cache rate bills full price", "g/no-rate", 1_000_000, 0, 400_000, 2.5, 2.5},
		{"cached clamped to input", "g/discounted", 100_000, 0, 1_000_000, 0.025, 0.025},
		{"negative cached clamped to zero", "g/discounted", 100_000, 0, -50, 0.25, 0.25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, _, total := service.CalculateCost(tt.model, tt.input, tt.output, tt.cached)
			assert.InDelta(t, tt.wantInput, in, 1e-9)
			assert.InDelta(t, tt.wantTotal, total, 1e-9)
		})
	}
}

// TestParseGatewayPricing covers the per-token decimal string → per-MTok
// float conversion at ingest.
func TestParseGatewayPricing(t *testing.T) {
	cache := "0.00000025"
	price, ok := parseGatewayPricing(&sdk.Pricing{
		InputPerToken: "0.0000025", OutputPerToken: "0.00001", CacheReadPerToken: &cache,
	})
	assert.True(t, ok)
	assert.InDelta(t, 2.5, price.inputPerMTok, 1e-9)
	assert.InDelta(t, 10.0, price.outputPerMTok, 1e-9)
	if assert.NotNil(t, price.cacheReadPerMTok) {
		assert.InDelta(t, 0.25, *price.cacheReadPerMTok, 1e-9)
	}

	price, ok = parseGatewayPricing(&sdk.Pricing{InputPerToken: "0.0000025", OutputPerToken: "0.00001"})
	assert.True(t, ok)
	assert.Nil(t, price.cacheReadPerMTok)

	_, ok = parseGatewayPricing(nil)
	assert.False(t, ok)

	_, ok = parseGatewayPricing(&sdk.Pricing{InputPerToken: "not-a-number", OutputPerToken: "0.00001"})
	assert.False(t, ok)
}

func TestPricingService_CalculateCost(t *testing.T) {
	cfg := &config.PricingConfig{
		Enabled: true,
		CustomPrices: map[string]config.CustomPricing{
			"test-model": {
				InputPricePerMToken:  10.00,
				OutputPricePerMToken: 20.00,
			},
		},
	}

	service := NewPricingService(cfg)

	inputCost, outputCost, totalCost := service.CalculateCost("test-model", 100000, 50000, 0)

	expectedInputCost := (100000.0 / 1_000_000.0) * 10.00       // $1.00
	expectedOutputCost := (50000.0 / 1_000_000.0) * 20.00       // $1.00
	expectedTotalCost := expectedInputCost + expectedOutputCost // $2.00

	assert.InDelta(t, expectedInputCost, inputCost, 0.01)
	assert.InDelta(t, expectedOutputCost, outputCost, 0.01)
	assert.InDelta(t, expectedTotalCost, totalCost, 0.01)
}
