package config_test

import (
	"os"
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

	t.Run("empty dir defaults to ~/.infer/tts", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home dir")
		}
		got, err := config.TextToSpeechConfig{}.ResolveOutputDir()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, config.ConfigDirName, "tts")
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

	t.Run("unknown engine is rejected", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.TextToSpeech.Engine = "piper"
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for unknown text_to_speech.engine")
		}
	})
}

func TestTextToSpeechRequireApproval(t *testing.T) {
	t.Run("unset means no approval", func(t *testing.T) {
		cfg := config.DefaultConfig()
		if cfg.IsApprovalRequired("TextToSpeech") {
			t.Error("TextToSpeech should not require approval by default")
		}
	})

	t.Run("opt in via config", func(t *testing.T) {
		cfg := config.DefaultConfig()
		require := true
		cfg.TextToSpeech.RequireApproval = &require
		if !cfg.IsApprovalRequired("TextToSpeech") {
			t.Error("text_to_speech.require_approval: true must be honoured")
		}
	})

	t.Run("explicit false stays exempt", func(t *testing.T) {
		cfg := config.DefaultConfig()
		require := false
		cfg.TextToSpeech.RequireApproval = &require
		if cfg.IsApprovalRequired("TextToSpeech") {
			t.Error("text_to_speech.require_approval: false must be honoured")
		}
	})
}

func TestDefaultConfigTextToSpeech(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.TextToSpeech.Enabled {
		t.Error("text_to_speech must default to disabled so the tool is absent from the LLM payload")
	}
	if !cfg.TextToSpeech.AutoDownload {
		t.Error("text_to_speech.auto_download should default to true")
	}
	if cfg.TextToSpeech.Timeout != 300 {
		t.Errorf("text_to_speech.timeout = %d, want 300", cfg.TextToSpeech.Timeout)
	}
	if cfg.TextToSpeech.Engine != config.TextToSpeechEngineQwen3 {
		t.Errorf("text_to_speech.engine = %q, want %q", cfg.TextToSpeech.Engine, config.TextToSpeechEngineQwen3)
	}
}
