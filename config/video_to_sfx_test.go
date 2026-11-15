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

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		assert.Equal(t, "mirelo/sfx-1.6", VideoToSFXConfig{Model: " mirelo/sfx-1.6 "}.ResolveGatewayModel())
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

func TestVideoToSFXValidation(t *testing.T) {
	tests := []struct {
		name    string
		video   VideoToSFXConfig
		wantErr string
	}{
		{"defaults pass", VideoToSFXConfig{}, ""},
		{"explicit model passes", VideoToSFXConfig{Model: "mirelo/sfx-1.6"}, ""},
		{"bare model rejected", VideoToSFXConfig{Model: "sfx-2.0"}, "invalid video_to_sfx.model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.VideoToSFX = tt.video
			cfg.VideoToSFX.Enabled = true
			err := cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}

	t.Run("disabled feature skips validation", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.VideoToSFX = VideoToSFXConfig{Model: "sfx"}
		assert.NoError(t, cfg.Validate())
	})
}

func TestVideoToSFXDubGates(t *testing.T) {
	tests := []struct {
		name            string
		textToVideo     bool
		enabled         bool
		dub             bool
		wantDub         bool
		wantVideosRoute bool
	}{
		{name: "everything off"},
		{name: "dub without the section stays off", dub: true},
		{name: "the section alone does not dub", enabled: true},
		{name: "section and dub opt DubVideo in", enabled: true, dub: true, wantDub: true, wantVideosRoute: true},
		{name: "rendering alone needs the Videos API", textToVideo: true, wantVideosRoute: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.TextToVideo.Enabled = tt.textToVideo
			cfg.VideoToSFX.Enabled = tt.enabled
			cfg.VideoToSFX.Dub = tt.dub

			assert.Equal(t, tt.wantDub, cfg.VideoToSFX.DubEnabled())
			assert.Equal(t, tt.wantVideosRoute, cfg.NeedsGatewayVideos())
		})
	}
}
