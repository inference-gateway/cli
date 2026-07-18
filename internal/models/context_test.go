package models

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
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

func TestClaudeContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"anthropic/claude-opus-4-7", 1000000},
		{"anthropic/claude-opus-4-8", 1000000},
		{"claude-fable-5", 1000000},
		{"anthropic/claude-fable-5", 1000000},
		{"anthropic/claude-sonnet-4-6", 200000},
		{"anthropic/claude-opus-4-6", 200000},
		{"anthropic/claude-opus-4-5-20251101", 200000},
		{"anthropic/claude-haiku-4-5-20251001", 200000},
		{"anthropic/claude-sonnet-4-5-20250929", 200000},
		{"anthropic/claude-opus-4-1-20250805", 200000},
		{"anthropic/claude-opus-4-20250514", 200000},
		{"anthropic/claude-sonnet-4-20250514", 200000},
	}

	for _, tc := range testModels {
		result := EstimateContextWindow(tc.model)
		t.Logf("Model: %-45s -> Context Window: %d (expected: %d)", tc.model, result, tc.expected)
		if result != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, result, tc.expected)
		}
	}
}

func TestMoonshotContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"moonshot/kimi-k2-thinking", 262144},
		{"moonshot/kimi-k2-0905-preview", 262144},
		{"moonshot/kimi-latest", 262144},
		{"moonshot/moonshot-v1-8k", 8192},
		{"moonshot/moonshot-v1-32k", 32768},
		{"moonshot/moonshot-v1-128k", 131072},
		{"moonshot/moonshot-v1-auto", 8192},
		{"moonshot/moonshot-v1-8k-vision-preview", 8192},
		{"moonshot/moonshot-v1-32k-vision-preview", 32768},
		{"moonshot/moonshot-v1-128k-vision-preview", 131072},
		{"moonshot/kimi-k2.5", 262144},
		{"moonshot/kimi-k2.6", 262144},
		{"moonshot/kimi-k2-thinking-turbo", 262144},
		{"moonshot/kimi-k2-turbo-preview", 262144},
		{"ollama_cloud/kimi-k2.5", 262144},
		{"ollama_cloud/kimi-k2.6", 262144},
	}

	for _, tc := range testModels {
		result := EstimateContextWindow(tc.model)
		t.Logf("Model: %-45s -> Context Window: %d (expected: %d)", tc.model, result, tc.expected)
		if result != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, result, tc.expected)
		}
	}
}

// TestGeminiContextWindow covers the Gemini family served via the gateway,
// including the 1M-token "*-latest" aliases that have to land on the
// gemini-pro / gemini-flash matchers, not the 32K generic gemini fallback.
func TestGeminiContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"google/models/gemini-3-pro-preview", 1048576},
		{"google/models/gemini-3-flash-preview", 1048576},
		{"google/models/gemini-3.1-pro-preview", 1048576},
		{"google/models/gemini-3.1-flash-lite-preview", 1048576},
		{"google/models/gemini-2.5-pro", 1000000},
		{"google/models/gemini-2.5-flash", 1000000},
		{"google/models/gemini-2.0-flash", 1000000},
		{"google/models/gemini-2.0-flash-lite", 1000000},
		{"google/models/gemini-pro-latest", 1000000},
		{"google/models/gemini-flash-latest", 1000000},
		{"google/models/gemini-flash-lite-latest", 1000000},
	}

	for _, tc := range testModels {
		if got := EstimateContextWindow(tc.model); got != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, got, tc.expected)
		}
	}
}

// TestDeepResearchContextWindow covers Google's Deep Research agents
// (Gemini 3.1 Pro backed, 1M input window, agentic via Interactions API).
func TestDeepResearchContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"google/models/deep-research-max-preview-04-2026", 1048576},
		{"google/models/deep-research-preview-04-2026", 1048576},
		{"google/models/deep-research-pro-preview-12-2025", 1048576},
	}

	for _, tc := range testModels {
		if got := EstimateContextWindow(tc.model); got != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, got, tc.expected)
		}
	}
}

// TestGemmaContextWindow covers Google's open-weight Gemma family. Gemma 3-1B
// and the 3n (Nano) variants are 32K; 4B/12B/27B and Gemma 4 extend to 128K.
func TestGemmaContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"google/models/gemma-3-1b-it", 32768},
		{"google/models/gemma-3-4b-it", 131072},
		{"google/models/gemma-3-12b-it", 131072},
		{"google/models/gemma-3-27b-it", 131072},
		{"google/models/gemma-3n-e4b-it", 32768},
		{"google/models/gemma-3n-e2b-it", 32768},
		{"google/models/gemma-4-26b-a4b-it", 131072},
		{"google/models/gemma-4-31b-it", 131072},
		{"ollama_cloud/gemma3:4b", 131072},
		{"ollama_cloud/gemma3:12b", 131072},
		{"ollama_cloud/gemma3:27b", 131072},
		{"ollama_cloud/gemma4:31b", 131072},
	}

	for _, tc := range testModels {
		if got := EstimateContextWindow(tc.model); got != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, got, tc.expected)
		}
	}
}

// TestDeepSeekContextWindow covers V3 (and minor releases like V3.2) plus the
// V4-pro/flash 1M-context tier.
func TestDeepSeekContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"ollama_cloud/deepseek-v3.2", 131072},
		{"ollama_cloud/deepseek-v4-pro", 1000000},
		{"ollama_cloud/deepseek-v4-flash", 1000000},
		{"deepseek/deepseek-v4-pro", 1000000},
		{"deepseek/deepseek-v4-flash", 1000000},
	}

	for _, tc := range testModels {
		if got := EstimateContextWindow(tc.model); got != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, got, tc.expected)
		}
	}
}

// TestMistralFamilyContextWindow covers Mistral Large 3, Ministral 3, and
// Devstral 2 variants served via ollama_cloud.
func TestMistralFamilyContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"ollama_cloud/mistral-large-3:675b", 131072},
		{"ollama_cloud/ministral-3:3b", 256000},
		{"ollama_cloud/ministral-3:8b", 256000},
		{"ollama_cloud/ministral-3:14b", 256000},
		{"ollama_cloud/devstral-2:123b", 131072},
		{"ollama_cloud/devstral-small-2:24b", 131072},
	}

	for _, tc := range testModels {
		if got := EstimateContextWindow(tc.model); got != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, got, tc.expected)
		}
	}
}

// TestMiscOpenWeightContextWindow covers GPT-OSS, MiniMax M2/M3, GLM 4/5, and
// Nemotron 3 - all served via ollama_cloud. MiniMax M2 is 204800 while M3
// jumps to a 1M window, so the two must resolve to distinct matchers.
func TestMiscOpenWeightContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"ollama_cloud/gpt-oss:20b", 131072},
		{"ollama_cloud/gpt-oss:120b", 131072},
		{"ollama_cloud/minimax-m2", 204800},
		{"ollama_cloud/minimax-m2.1", 204800},
		{"ollama_cloud/minimax-m2.5", 204800},
		{"ollama_cloud/minimax-m2.7", 204800},
		{"ollama_cloud/minimax-m3", 1000000},
		{"ollama_cloud/minimax-m3.1", 1000000},
		{"ollama_cloud/glm-4.6", 200000},
		{"ollama_cloud/glm-4.7", 200000},
		{"ollama_cloud/glm-5", 200000},
		{"ollama_cloud/glm-5.1", 200000},
		{"ollama_cloud/glm-5.2", 1000000},
		{"ollama_cloud/nemotron-3-super", 262144},
		{"ollama_cloud/nemotron-3-nano:30b", 262144},
	}

	for _, tc := range testModels {
		if got := EstimateContextWindow(tc.model); got != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, got, tc.expected)
		}
	}
}

// TestQwenServedVariants covers the Qwen3 sub-families served via
// ollama_cloud - VL, Coder, Next, and the qwen3.5 alias all match the
// "qwen3" matcher (262K).
func TestQwenServedVariants(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"ollama_cloud/qwen3-vl:235b", 262144},
		{"ollama_cloud/qwen3-vl:235b-instruct", 262144},
		{"ollama_cloud/qwen3-coder:480b", 262144},
		{"ollama_cloud/qwen3-coder-next", 262144},
		{"ollama_cloud/qwen3-next:80b", 262144},
		{"ollama_cloud/qwen3.5:397b", 262144},
	}

	for _, tc := range testModels {
		if got := EstimateContextWindow(tc.model); got != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, got, tc.expected)
		}
	}
}

// TestLocalFamilyContextWindow covers the generic llama/phi family fallbacks
// used by local providers (llamacpp, ollama) whose model names are arbitrary.
func TestLocalFamilyContextWindow(t *testing.T) {
	testModels := []struct {
		model    string
		expected int
	}{
		{"llamacpp/llama", 131072},
		{"ollama/llama3.2", 131072},
		{"ollama/codellama", 131072},
		{"llamacpp/phi", 16384},
		{"llamacpp/mistral", 32768},
		{"llamacpp/qwen", 128000},
	}

	for _, tc := range testModels {
		if got := EstimateContextWindow(tc.model); got != tc.expected {
			t.Errorf("Model %s: got %d, expected %d", tc.model, got, tc.expected)
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
		{"llamacpp/qwen2", 32768, true},               // override beats built-in qwen matcher (128000)
		{"llamacpp/qwen3-coder", 65536, true},         // longest pattern wins over "qwen"
		{"llamacpp/my-model-q4.gguf", 4096, true},     // case-insensitive, unknown model becomes known
		{"anthropic/claude-opus-4-8", 1000000, true},  // untouched models still use built-ins
		{"ollama_cloud/brand-new-model", 8192, false}, // no override, no matcher -> default
	}

	for _, tc := range testCases {
		size, known := LookupContextWindow(tc.model)
		if size != tc.expectedSize || known != tc.expectedKnown {
			t.Errorf("Model %s: got (%d, %v), expected (%d, %v)", tc.model, size, known, tc.expectedSize, tc.expectedKnown)
		}
	}
}

// TestLookupContextWindow_MatchedFlag covers the matched bool that the session
// rollover and auto-compaction gates rely on: known models report true, while
// models with no matcher report false (returning the default fallback as the
// size) so callers can disable context-based behavior instead of measuring
// fullness against a wrong window. minimax-m2 (204800) and minimax-m3 (1M)
// must resolve to their own distinct windows - the m2 pattern must not swallow
// m3, which was the original ollama_cloud/minimax-m3 bug.
func TestLookupContextWindow_MatchedFlag(t *testing.T) {
	testCases := []struct {
		model         string
		expectedKnown bool
		expectedSize  int
	}{
		{"ollama_cloud/minimax-m2", true, 204800},
		{"ollama_cloud/minimax-m3", true, 1000000},
		{"ollama_cloud/brand-new-model", false, 8192},
		{"openai/gpt-4", false, 8192},
		{"anthropic/claude-opus-4-7", true, 1000000},
		{"moonshot/moonshot-v1-8k", true, 8192},
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
