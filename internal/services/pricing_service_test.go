package services

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

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
		{"google/models/gemini-2.5-pro", "$1.25/$10.00 per MTok"},
		{"google/models/gemini-2.5-flash", "$0.30/$2.50 per MTok"},
		{"google/models/gemini-2.5-flash-lite", "$0.10/$0.40 per MTok"},
		{"google/models/gemini-2.0-flash", "$0.10/$0.40 per MTok"},
		{"google/models/gemini-2.0-flash-001", "$0.10/$0.40 per MTok"},
		{"google/models/gemini-2.0-flash-lite", "$0.07/$0.30 per MTok"},
		{"google/models/gemini-3-flash-preview", "$0.50/$3.00 per MTok"},
		{"google/models/gemini-3.1-pro-preview", "$2.00/$12.00 per MTok"},
		{"google/models/gemma-3-1b-it", "free"},
		{"google/models/gemma-3-27b-it", "free"},
		{"google/models/gemma-3n-e2b-it", "free"},
		{"google/models/gemma-4-31b-it", "free"},
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
			name:     "pricing disabled returns false even for pro default",
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
			name:     "default pro model returns true",
			enabled:  true,
			model:    "ollama_cloud/deepseek-v4-pro",
			expected: true,
		},
		{
			name:     "default flash pro model returns true",
			enabled:  true,
			model:    "ollama_cloud/deepseek-v4-flash",
			expected: true,
		},
		{
			name:     "default free ollama cloud model returns false",
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

// TestPricingService_OllamaCloudProDefaults guards the curated set of Ollama
// Cloud models that are gated behind a Pro subscription. Only the two named in
// issue #590 should be flagged; the rest of the ollama_cloud catalog is free.
func TestPricingService_OllamaCloudProDefaults(t *testing.T) {
	proModels := []string{
		"ollama_cloud/deepseek-v4-pro",
		"ollama_cloud/deepseek-v4-flash",
	}
	for _, model := range proModels {
		t.Run("pro/"+model, func(t *testing.T) {
			entry, ok := config.DefaultModelPricing[model]
			assert.True(t, ok, "expected default pricing entry for %s", model)
			assert.True(t, entry.RequiresPro, "expected %s to require Pro", model)
		})
	}

	freeModels := []string{
		"ollama_cloud/kimi-k2.5",
		"ollama_cloud/qwen3-coder:480b",
		"ollama_cloud/gpt-oss:120b",
		"ollama_cloud/glm-5.1",
		"ollama_cloud/deepseek-v3.2",
	}
	for _, model := range freeModels {
		t.Run("free/"+model, func(t *testing.T) {
			entry, ok := config.DefaultModelPricing[model]
			assert.True(t, ok, "expected default pricing entry for %s", model)
			assert.False(t, entry.RequiresPro, "expected %s to be free, not Pro", model)
		})
	}
}

// TestFormatModelPricingLabel exercises the shared label helper that the model
// view and autocomplete both use: it must suppress the misleading "free" token
// for Pro models and keep the price for paid+Pro models.
func TestFormatModelPricingLabel(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		model        string
		customPrices map[string]config.CustomPricing
		expected     string
	}{
		{
			name:     "default pro model shows pro marker, free suppressed",
			enabled:  true,
			model:    "ollama_cloud/deepseek-v4-pro",
			expected: "pro subscription",
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
			name:    "paid and pro model shows both",
			enabled: true,
			model:   "paid-pro-model",
			customPrices: map[string]config.CustomPricing{
				"paid-pro-model": {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, RequiresPro: true},
			},
			expected: "$3.00/$15.00 per MTok, pro subscription",
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

	inputCost, outputCost, totalCost := service.CalculateCost("test-model", 100000, 50000)

	expectedInputCost := (100000.0 / 1_000_000.0) * 10.00       // $1.00
	expectedOutputCost := (50000.0 / 1_000_000.0) * 20.00       // $1.00
	expectedTotalCost := expectedInputCost + expectedOutputCost // $2.00

	assert.InDelta(t, expectedInputCost, inputCost, 0.01)
	assert.InDelta(t, expectedOutputCost, outputCost, 0.01)
	assert.InDelta(t, expectedTotalCost, totalCost, 0.01)
}
