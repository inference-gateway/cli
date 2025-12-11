package services

import (
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/stretchr/testify/assert"
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
			model:   "deepseek-chat",
			customPrices: map[string]config.CustomPricing{
				"deepseek-chat": {
					InputPricePerMToken:  0.14,
					OutputPricePerMToken: 0.28,
				},
			},
			expectedOutput: "$0.14/$0.28 per MTok",
		},
		{
			name:           "unknown model returns free",
			enabled:        true,
			model:          "unknown-model",
			customPrices:   map[string]config.CustomPricing{},
			expectedOutput: "free",
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

	// Test with 100,000 input tokens and 50,000 output tokens
	inputCost, outputCost, totalCost := service.CalculateCost("test-model", 100000, 50000)

	expectedInputCost := (100000.0 / 1_000_000.0) * 10.00       // $1.00
	expectedOutputCost := (50000.0 / 1_000_000.0) * 20.00       // $1.00
	expectedTotalCost := expectedInputCost + expectedOutputCost // $2.00

	assert.InDelta(t, expectedInputCost, inputCost, 0.01)
	assert.InDelta(t, expectedOutputCost, outputCost, 0.01)
	assert.InDelta(t, expectedTotalCost, totalCost, 0.01)
}
