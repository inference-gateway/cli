package config_test

import (
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestResolveTextToSpeechOutputDir(t *testing.T) {
	t.Run("explicit dir returned as-is", func(t *testing.T) {
		cfg := config.TextToSpeechConfig{OutputDir: "/tmp/custom-tts"}
		got, err := cfg.ResolveOutputDir()
		if err != nil {
			t.Fatal(err)
		}
		if got != "/tmp/custom-tts" {
			t.Errorf("got %q, want /tmp/custom-tts", got)
		}
	})

	t.Run("empty dir defaults to the project's media/tts", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		got, err := config.TextToSpeechConfig{}.ResolveOutputDir()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(config.ProjectTmpDir(), "media", "tts")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestValidateTextToSpeechEngine(t *testing.T) {
	t.Run("empty engine is the default", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.TextToSpeech.Engine = ""
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("qwen3-tts is supported", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.TextToSpeech.Engine = config.TextToSpeechEngineQwen3
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("gateway is supported with a provider/model", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.TextToSpeech.Engine = config.TextToSpeechEngineGateway
		cfg.TextToSpeech.Model = "openai/gpt-4o-mini-tts"
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("gateway defaults an empty model to local/qwen3-tts", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.TextToSpeech.Engine = config.TextToSpeechEngineGateway
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if got := cfg.TextToSpeech.ResolveGatewayModel(); got != config.TextToSpeechGatewayDefaultModel {
			t.Errorf("ResolveGatewayModel() = %q, want %q", got, config.TextToSpeechGatewayDefaultModel)
		}
	})

	t.Run("gateway rejects a model without a provider", func(t *testing.T) {
		for _, model := range []string{"q8", "openai/", "/tts-1"} {
			cfg := &config.Config{}
			cfg.TextToSpeech.Enabled = true
			cfg.TextToSpeech.Engine = config.TextToSpeechEngineGateway
			cfg.TextToSpeech.Model = model
			if err := cfg.Validate(); err == nil {
				t.Errorf("expected error for gateway engine with model %q", model)
			}
		}
	})

	t.Run("unknown engine is rejected", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.TextToSpeech.Enabled = true
		cfg.TextToSpeech.Engine = "piper"
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for unknown text_to_speech.engine")
		}
	})

	t.Run("unknown engine or bad model is ignored when disabled", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			engine string
			model  string
		}{
			{"engine from a newer binary", config.TextToSpeechEngineGateway, ""},
			{"unknown engine", "piper", ""},
			{"bad gateway model", config.TextToSpeechEngineGateway, "q8"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.TextToSpeech.Enabled = false
				cfg.TextToSpeech.Engine = tt.engine
				cfg.TextToSpeech.Model = tt.model
				if err := cfg.Validate(); err != nil {
					t.Errorf("unexpected error for disabled text_to_speech: %v", err)
				}
			})
		}
	})
}
