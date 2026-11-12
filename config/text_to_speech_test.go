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

func TestValidateTextToSpeechModel(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		engine    string
		model     string
		wantModel string
		wantErr   bool
	}{
		{"default model", true, "", "", config.TextToSpeechGatewayDefaultModel, false},
		{"provider/model", true, "", "openai/gpt-4o-mini-tts", "openai/gpt-4o-mini-tts", false},
		{"gateway engine still accepted", true, "gateway", "elevenlabs/eleven_v3", "elevenlabs/eleven_v3", false},
		{"model without a provider", true, "", "q8", "q8", true},
		{"provider without a model", true, "", "openai/", "openai/", true},
		{"model without a provider name", true, "", "/tts-1", "/tts-1", true},
		{"removed engine falls back to the default", true, "qwen3-tts", "q8", config.TextToSpeechGatewayDefaultModel, false},
		{"unknown engine falls back to the default", true, "piper", "", config.TextToSpeechGatewayDefaultModel, false},
		{"bad model is ignored when disabled", false, "", "q8", "q8", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.TextToSpeech.Enabled = tt.enabled
			cfg.TextToSpeech.Engine = tt.engine
			cfg.TextToSpeech.Model = tt.model
			if err := cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := cfg.TextToSpeech.ResolveGatewayModel(); got != tt.wantModel {
				t.Errorf("ResolveGatewayModel() = %q, want %q", got, tt.wantModel)
			}
		})
	}
}

func TestTextToSpeechNamesRemovedEngine(t *testing.T) {
	for engine, want := range map[string]bool{"": false, "gateway": false, " gateway ": false, "qwen3-tts": true, "piper": true} {
		if got := (config.TextToSpeechConfig{Engine: engine}).NamesRemovedEngine(); got != want {
			t.Errorf("NamesRemovedEngine(%q) = %v, want %v", engine, got, want)
		}
	}
}

func TestDefaultTextToSpeechModelIsFullyQualified(t *testing.T) {
	if got := config.DefaultConfig().TextToSpeech.Model; got != "local/qwen3-tts" {
		t.Errorf("default text_to_speech.model = %q, want local/qwen3-tts", got)
	}
}
