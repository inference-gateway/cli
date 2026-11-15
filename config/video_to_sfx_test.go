package config

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
)

func TestVideoToSFXResolveGatewayModel(t *testing.T) {
	assert.Equal(t, VideoToSFXGatewayDefaultModel, VideoToSFXConfig{}.ResolveGatewayModel())

	cfg := VideoToSFXConfig{Model: " mirelo/sfx-1.6 "}
	assert.Equal(t, "mirelo/sfx-1.6", cfg.ResolveGatewayModel())
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
