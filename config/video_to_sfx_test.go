package config

import (
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestVideoToSFXConfigResolveGatewayModel(t *testing.T) {
	t.Run("default is the gateway's mirelo video model", func(t *testing.T) {
		assert.Equal(t, VideoToSFXGatewayDefaultModel, VideoToSFXConfig{}.ResolveGatewayModel())
	})

	t.Run("a configured model passes through", func(t *testing.T) {
		assert.Equal(t, "mirelo/sfx-1.5", VideoToSFXConfig{Model: "mirelo/sfx-1.5"}.ResolveGatewayModel())
	})
}

func TestVideoToSFXConfigResolveOutputDir(t *testing.T) {
	t.Run("custom output dir is honored", func(t *testing.T) {
		got, err := VideoToSFXConfig{OutputDir: "/tmp/custom-sfx"}.ResolveOutputDir()
		require.NoError(t, err)
		assert.Equal(t, "/tmp/custom-sfx", got)
	})

	t.Run("default is the project's media/sfx shelf", func(t *testing.T) {
		t.Setenv("HOME", "/home/fake")
		got, err := VideoToSFXConfig{}.ResolveOutputDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(ProjectTmpDir(), "media", "sfx"), got)
	})
}

func TestVideoToSFXModelValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VideoToSFX.Enabled = true

	t.Run("default model passes", func(t *testing.T) {
		cfg.VideoToSFX.Model = ""
		assert.NoError(t, cfg.Validate())
		assert.Equal(t, VideoToSFXGatewayDefaultModel, cfg.VideoToSFX.ResolveGatewayModel())
	})

	t.Run("model without provider fails", func(t *testing.T) {
		cfg.VideoToSFX.Model = "sfx-2.0"
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "video_to_sfx.model")
	})
}
