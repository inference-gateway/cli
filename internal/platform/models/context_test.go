package models

import (
	"testing"

	sdk "github.com/inference-gateway/sdk"

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

// TestIsChatCapableModalities covers the chat-capability predicate used to
// filter the model picker: a model must accept text input and produce text
// output. Vision chat models pass; image generation, speech-to-text,
// text-to-speech, and empty/unknown modality sets do not.
func TestIsChatCapableModalities(t *testing.T) {
	testCases := []struct {
		name string
		mods sdk.ModelModalities
		want bool
	}{
		{"text to text chat", sdk.ModelModalities{Input: []sdk.Modality{sdk.ModalityText}, Output: []sdk.Modality{sdk.ModalityText}}, true},
		{"vision chat (image in, text out)", sdk.ModelModalities{Input: []sdk.Modality{sdk.ModalityText, sdk.ModalityImage}, Output: []sdk.Modality{sdk.ModalityText}}, true},
		{"image generation (text in, image out)", sdk.ModelModalities{Input: []sdk.Modality{sdk.ModalityText}, Output: []sdk.Modality{sdk.ModalityImage}}, false},
		{"speech-to-text (audio in, text out)", sdk.ModelModalities{Input: []sdk.Modality{sdk.ModalityAudio}, Output: []sdk.Modality{sdk.ModalityText}}, false},
		{"text-to-speech (text in, audio out)", sdk.ModelModalities{Input: []sdk.Modality{sdk.ModalityText}, Output: []sdk.Modality{sdk.ModalityAudio}}, false},
		{"empty modalities (unknown)", sdk.ModelModalities{}, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsChatCapableModalities(tc.mods); got != tc.want {
				t.Errorf("IsChatCapableModalities(%v) = %v, expected %v", tc.mods, got, tc.want)
			}
		})
	}
}

// TestCapabilityPredicatesAndLabels covers the per-model capability lookups
// behind the model picker's capability tabs and row suffixes: vision is
// image *input*, audio/video match either side (so STT and TTS both count as
// audio), and ModalitiesLabel comma-joins the extra modalities.
func TestCapabilityPredicatesAndLabels(t *testing.T) {
	SetGatewayModalities(map[string]sdk.ModelModalities{
		"p/chat":      {Input: []sdk.Modality{sdk.ModalityText}, Output: []sdk.Modality{sdk.ModalityText}},
		"p/vision":    {Input: []sdk.Modality{sdk.ModalityText, sdk.ModalityImage}, Output: []sdk.Modality{sdk.ModalityText}},
		"p/omni":      {Input: []sdk.Modality{sdk.ModalityText, sdk.ModalityImage, sdk.ModalityAudio, sdk.ModalityVideo}, Output: []sdk.Modality{sdk.ModalityText}},
		"p/stt":       {Input: []sdk.Modality{sdk.ModalityAudio}, Output: []sdk.Modality{sdk.ModalityText}},
		"p/tts":       {Input: []sdk.Modality{sdk.ModalityText}, Output: []sdk.Modality{sdk.ModalityAudio}},
		"p/image-gen": {Input: []sdk.Modality{sdk.ModalityText}, Output: []sdk.Modality{sdk.ModalityImage}},
	})
	defer SetGatewayModalities(nil)

	testCases := []struct {
		model                string
		vision, audio, video bool
		label                string
	}{
		{"p/chat", false, false, false, ""},
		{"p/vision", true, false, false, "vision"},
		{"p/omni", true, true, true, "vision, audio, video"},
		{"p/stt", false, true, false, "audio"},
		{"p/tts", false, true, false, "audio"},
		{"p/image-gen", false, false, false, "image-gen"},
		{"p/unknown", false, false, false, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.model, func(t *testing.T) {
			if got := SupportsVision(tc.model); got != tc.vision {
				t.Errorf("SupportsVision = %v, expected %v", got, tc.vision)
			}
			if got := SupportsAudio(tc.model); got != tc.audio {
				t.Errorf("SupportsAudio = %v, expected %v", got, tc.audio)
			}
			if got := SupportsVideo(tc.model); got != tc.video {
				t.Errorf("SupportsVideo = %v, expected %v", got, tc.video)
			}
			if got := ModalitiesLabel(tc.model); got != tc.label {
				t.Errorf("ModalitiesLabel = %q, expected %q", got, tc.label)
			}
		})
	}
}

// TestNonChatModels covers the view-only registry: setters replace the list
// and callers get a copy they cannot mutate in place.
func TestNonChatModels(t *testing.T) {
	SetNonChatModels([]string{"p/stt", "p/tts"})
	defer SetNonChatModels(nil)

	got := NonChatModels()
	if len(got) != 2 || got[0] != "p/stt" || got[1] != "p/tts" {
		t.Errorf("NonChatModels() = %v", got)
	}
	got[0] = "mutated"
	if NonChatModels()[0] != "p/stt" {
		t.Error("NonChatModels must return a copy")
	}

	if !IsNonChatModel("p/stt") || IsNonChatModel("p/chat") {
		t.Error("IsNonChatModel must reflect the registered list")
	}
}
