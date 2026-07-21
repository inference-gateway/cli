package models

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// TestUserContextWindowOverride covers the config.yaml `context_windows:`
// override map: it wins over gateway data, the longest matching pattern is
// picked deterministically, and unknown models return (0, false). Local
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
		{"llamacpp/qwen2", 32768, true},           // override catches qwen2
		{"llamacpp/qwen3-coder", 65536, true},     // longest pattern wins over "qwen"
		{"llamacpp/my-model-q4.gguf", 4096, true}, // case-insensitive, unknown model becomes known
		{"anthropic/claude-opus-4-8", 0, false},   // no override, no gateway data -> unknown
		{"ollama_cloud/brand-new-model", 0, false},
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

	if size, known := LookupContextWindow("gpt-4"); size != 0 || known {
		t.Errorf("bare name must miss gateway entry: got (%d, %v), expected (0, false)", size, known)
	}

	config.UserContextWindows = map[string]int{"gpt-4": 12345}
	defer func() { config.UserContextWindows = nil }()
	if size, known := LookupContextWindow("openai/gpt-4"); size != 12345 || !known {
		t.Errorf("user override must win over gateway: got (%d, %v), expected (12345, true)", size, known)
	}
	config.UserContextWindows = nil

	if size, known := LookupContextWindow("anthropic/claude-opus-4-8"); size != 0 || known {
		t.Errorf("unknown model must report (0, false): got (%d, %v)", size, known)
	}
}

// TestLookupContextWindow_MatchedFlag covers the matched bool that the session
// rollover and auto-compaction gates rely on: models with no user override and
// no gateway entry report (0, false) so callers disable context-based behavior
// instead of measuring fullness against a wrong window.
func TestLookupContextWindow_MatchedFlag(t *testing.T) {
	for _, model := range []string{
		"ollama_cloud/brand-new-model",
		"openai/gpt-4",
		"anthropic/claude-opus-4-7",
	} {
		size, known := LookupContextWindow(model)
		if known || size != 0 {
			t.Errorf("Model %s: got (%d, %v), expected (0, false)", model, size, known)
		}
	}
}
