package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestResolveRecordingsDir(t *testing.T) {
	t.Run("explicit dir returned as-is", func(t *testing.T) {
		cfg := config.SpeechToTextConfig{RecordingsDir: "/tmp/custom-voice"}
		got, err := cfg.ResolveRecordingsDir()
		if err != nil {
			t.Fatal(err)
		}
		if got != "/tmp/custom-voice" {
			t.Errorf("got %q, want /tmp/custom-voice", got)
		}
	})

	t.Run("empty dir defaults to ~/.infer/voice", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home dir")
		}
		got, err := config.SpeechToTextConfig{}.ResolveRecordingsDir()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, config.ConfigDirName, "voice")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestValidateRetainRecordings(t *testing.T) {
	t.Run("negative is rejected", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.SpeechToText.RetainRecordings = -1
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for negative retain_recordings")
		}
	})

	t.Run("non-negative is accepted", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.SpeechToText.RetainRecordings = 10
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
