package config

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
)

func TestTextToVideoResolveGatewayModel(t *testing.T) {
	assert.Equal(t, TextToVideoGatewayDefaultModel, TextToVideoConfig{}.ResolveGatewayModel(false))
	assert.Equal(t, TextToVideoGatewayDefaultAvatarModel, TextToVideoConfig{}.ResolveGatewayModel(true))

	cfg := TextToVideoConfig{Model: " google/veo-3 ", AvatarModel: "acme/talking-head"}
	assert.Equal(t, "google/veo-3", cfg.ResolveGatewayModel(false))
	assert.Equal(t, "acme/talking-head", cfg.ResolveGatewayModel(true))
}

func TestTextToVideoValidation(t *testing.T) {
	tests := []struct {
		name    string
		video   TextToVideoConfig
		wantErr string
	}{
		{"defaults pass", TextToVideoConfig{}, ""},
		{"explicit models pass", TextToVideoConfig{Model: "elevenlabs/bytedance-seedance-v2", AvatarModel: "elevenlabs/creatify-aurora"}, ""},
		{"bare model rejected", TextToVideoConfig{Model: "veo"}, "invalid text_to_video.model"},
		{"bare avatar model rejected", TextToVideoConfig{AvatarModel: "creatify-aurora"}, "invalid text_to_video.avatar_model"},
		{"negative poll interval rejected", TextToVideoConfig{PollInterval: -1}, "must not be negative"},
		{"negative timeout rejected", TextToVideoConfig{Timeout: -5}, "must not be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.TextToVideo = tt.video
			cfg.TextToVideo.Enabled = true
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
		cfg.TextToVideo = TextToVideoConfig{Model: "veo", PollInterval: -1}
		assert.NoError(t, cfg.Validate())
	})
}
