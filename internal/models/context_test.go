package models

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestProviderPrefixStripping(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"ollama_cloud/qwen3-coder:480b", 8192},
		{"openai/gpt-4", 8192},
		{"anthropic/claude-3", 8192},
	}

	for _, tc := range testModels {
		result := EstimateContextWindow(tc.model)
		if result != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, result, tc.expected)
		}
	}
}

// TestUserContextWindowOverride covers the config.yaml `context_windows:`
// override map: it wins over built-in matchers, the longest matching pattern
// is picked deterministically, and an empty map falls through unchanged. Local
// servers (llama.cpp -c flag) can run any context size, so users need to be
// able to declare the truth per deployment.
func TestUserContextWindowOverride(t *testing.T) {
	config.UserContextWindows = map[string]int{
		"qwen":     32768,
		"qwen3":    65536,
		"My-Model": 4096,
	}
	defer func() { config.UserContextWindows = nil }()

	testCases := []struct {
		model         string
		expectedSize  int
		expectedKnown bool
	}{
		{"llamacpp/qwen2", 32768, true},               // override catches qwen2
		{"llamacpp/qwen3-coder", 65536, true},         // longest pattern wins over "qwen"
		{"llamacpp/my-model-q4.gguf", 4096, true},     // case-insensitive, unknown model becomes known
		{"anthropic/claude-opus-4-8", 8192, false},    // no override, no matcher -> default
		{"ollama_cloud/brand-new-model", 8192, false}, // no override, no matcher -> default
	}

	for _, tc := range testCases {
		size, known := LookupContextWindow(tc.model)
		if size != tc.expectedSize || known != tc.expectedKnown {
			t.Errorf("Model %s: got (%d, %v), expected (%d, %v)", tc.model, size, known, tc.expectedSize, tc.expectedKnown)
		}
	}
}

// TestGatewayContextWindows covers the /v1/models?include=context_window
// registry: gateway data turns unknown models known, user config overrides
// still win, and keying is exact on the lowercased full "provider/model" id
// (never the stripped model name).
func TestGatewayContextWindows(t *testing.T) {
	SetGatewayContextWindows(map[string]int{"OpenAI/GPT-4": 400000})
	defer SetGatewayContextWindows(nil)

	if size, known := LookupContextWindow("openai/gpt-4"); size != 400000 || !known {
		t.Errorf("gateway window: got (%d, %v), expected (400000, true)", size, known)
	}

	if size, known := LookupContextWindow("gpt-4"); size != 8192 || known {
		t.Errorf("bare name must miss gateway entry: got (%d, %v), expected (8192, false)", size, known)
	}

	config.UserContextWindows = map[string]int{"gpt-4": 12345}
	defer func() { config.UserContextWindows = nil }()
	if size, known := LookupContextWindow("openai/gpt-4"); size != 12345 || !known {
		t.Errorf("user override must win over gateway: got (%d, %v), expected (12345, true)", size, known)
	}
	config.UserContextWindows = nil

	if size, known := LookupContextWindow("anthropic/claude-opus-4-8"); size != 8192 || known {
		t.Errorf("unknown model must fall through to default: got (%d, %v), expected (8192, false)", size, known)
	}
}

// TestLookupContextWindow_MatchedFlag covers the matched bool that the session
// rollover and auto-compaction gates rely on: known models report true, while
// models with no matcher report false (returning the default fallback as the
// size) so callers can disable context-based behavior instead of measuring
// fullness against a wrong window.
func TestLookupContextWindow_MatchedFlag(t *testing.T) {
	testCases := []struct {
		model         string
		expectedKnown bool
		expectedSize  int
	}{
		{"ollama_cloud/brand-new-model", false, 8192},
		{"openai/gpt-4", false, 8192},
		{"anthropic/claude-opus-4-7", false, 8192},
	}

	for _, tc := range testCases {
		size, known := LookupContextWindow(tc.model)
		if known != tc.expectedKnown {
			t.Errorf("Model %s: known=%v, expected %v", tc.model, known, tc.expectedKnown)
		}
		if size != tc.expectedSize {
			t.Errorf("Model %s: size=%d, expected %d", tc.model, size, tc.expectedSize)
		}
	}
}
