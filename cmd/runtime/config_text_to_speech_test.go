package runtime

import (
	"strings"
	"testing"

	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
)

// INFER_TEXT_TO_SPEECH_* env vars must override the text_to_speech config
// section through the shared viper layer, exactly like speech_to_text.
func TestTextToSpeechConfigEnvOverrides(t *testing.T) {
	t.Setenv("INFER_TEXT_TO_SPEECH_ENABLED", "true")
	t.Setenv("INFER_TEXT_TO_SPEECH_MODEL", "q8")
	t.Setenv("INFER_TEXT_TO_SPEECH_TIMEOUT", "120")
	t.Setenv("INFER_TEXT_TO_SPEECH_OUTPUT_DIR", "/tmp/tts-out")

	v := viper.New()
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	cfg := &config.Config{}
	resolveViperEnvironmentVariables(v, cfg, "")

	if !cfg.TextToSpeech.Enabled {
		t.Error("INFER_TEXT_TO_SPEECH_ENABLED=true should enable text-to-speech")
	}
	if cfg.TextToSpeech.Model != "q8" {
		t.Errorf("Model = %q, want q8", cfg.TextToSpeech.Model)
	}
	if cfg.TextToSpeech.Timeout != 120 {
		t.Errorf("Timeout = %d, want 120 (from INFER_TEXT_TO_SPEECH_TIMEOUT)", cfg.TextToSpeech.Timeout)
	}
	if cfg.TextToSpeech.OutputDir != "/tmp/tts-out" {
		t.Errorf("OutputDir = %q, want /tmp/tts-out", cfg.TextToSpeech.OutputDir)
	}
}
