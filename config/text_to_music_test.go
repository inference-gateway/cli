package config

import (
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestResolveTextToMusicOutputDir(t *testing.T) {
	t.Run("custom output dir is honored", func(t *testing.T) {
		cfg := TextToMusicConfig{OutputDir: "/tmp/custom-music"}
		got, err := cfg.ResolveOutputDir()
		require.NoError(t, err)
		assert.Equal(t, "/tmp/custom-music", got)
	})

	t.Run("default is ~/.infer/tmp/music", func(t *testing.T) {
		t.Setenv("HOME", "/home/fake")
		got, err := TextToMusicConfig{}.ResolveOutputDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("/home/fake", ConfigDirName, "tmp", "music"), got)
	})
}

func TestTextToMusicModelValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TextToMusic.Enabled = true

	t.Run("default model passes", func(t *testing.T) {
		cfg.TextToMusic.Model = ""
		assert.NoError(t, cfg.Validate())
		assert.Equal(t, TextToMusicGatewayDefaultModel, cfg.TextToMusic.ResolveGatewayModel())
	})

	t.Run("explicit provider/model passes", func(t *testing.T) {
		cfg.TextToMusic.Model = "stabilityai/stable-audio"
		assert.NoError(t, cfg.Validate())
	})

	t.Run("model without provider fails", func(t *testing.T) {
		cfg.TextToMusic.Model = "music_v2_5"
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "text_to_music.model")
	})
}
