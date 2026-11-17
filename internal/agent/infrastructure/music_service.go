package infrastructure

import (
	"context"
	"fmt"
	"os"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

// MusicService composes music clips through the gateway's Music API
// (POST /v1/audio/music), so requests show up in gateway logs and traces.
// It implements agentdomain.MusicService.
type MusicService struct {
	config *config.Config
	client sdk.Client
}

// NewMusicService creates a new gateway-backed music service.
func NewMusicService(cfg *config.Config, client sdk.Client) *MusicService {
	return &MusicService{
		config: cfg,
		client: client,
	}
}

// Compose generates an MP3 clip for prompt with text_to_music.model and writes
// it to outPath. A non-nil seconds caps the length and a non-nil instrumental
// asks for no vocals. Errors name the model so a gateway or provider without
// the endpoint is diagnosable.
func (s *MusicService) Compose(ctx context.Context, prompt, outPath string, seconds *float32, instrumental *bool) error {
	model := s.config.TextToMusic.ResolveGatewayModel()
	provider, modelName, ok := strings.Cut(model, "/")
	if !ok || provider == "" || modelName == "" {
		return fmt.Errorf("invalid text_to_music.model %q (expected 'provider/model')", model)
	}

	format := sdk.CreateMusicRequestResponseFormatMp3
	request := sdk.CreateMusicRequest{
		Prompt:          prompt,
		Model:           modelName,
		ResponseFormat:  &format,
		DurationSeconds: seconds,
		Instrumental:    instrumental,
	}

	audio, err := s.client.CreateMusic(ctx, sdk.Provider(provider), request)
	if err != nil {
		return fmt.Errorf("music generation with %s failed: %w", model, err)
	}
	if len(audio) == 0 {
		return fmt.Errorf("music generation with %s returned no audio", model)
	}

	if err := os.WriteFile(outPath, audio, 0o644); err != nil { // nolint:gosec
		return fmt.Errorf("writing audio to %q: %w", outPath, err)
	}
	return nil
}
