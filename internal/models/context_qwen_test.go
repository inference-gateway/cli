package models

import (
	"testing"
)

func TestQwenContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"ollama_cloud/qwen3-coder:480b", 262144},
		{"qwen3-coder:480b", 262144},
		{"qwen3-coder", 262144},
		{"qwen", 128000},
		{"qwen2", 128000},
		{"qwen2.5", 131072},
	}

	for _, tc := range testModels {
		result := EstimateContextWindow(tc.model)
		t.Logf("Model: %-35s -> Context Window: %d (expected: %d)", tc.model, result, tc.expected)
		if result != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, result, tc.expected)
		}
	}
}

func TestProviderPrefixStripping(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"ollama_cloud/qwen3-coder:480b", 262144},
		{"openai/gpt-4", 8192},
		{"anthropic/claude-3", 200000},
	}

	for _, tc := range testModels {
		result := EstimateContextWindow(tc.model)
		if result != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, result, tc.expected)
		}
	}
}
