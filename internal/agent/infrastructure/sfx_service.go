package infrastructure

import (
	"context"
	"fmt"
	"os"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

// SFXService generates sound effects through the gateway's SFX API
// (POST /v1/audio/sfx), so requests show up in gateway logs and traces.
// It implements agentdomain.SoundEffectService.
type SFXService struct {
	config *config.Config
	client sdk.Client
}

// NewSFXService creates a new gateway-backed sound-effect service.
func NewSFXService(cfg *config.Config, client sdk.Client) *SFXService {
	return &SFXService{
		config: cfg,
		client: client,
	}
}

// Generate produces a sound effect for prompt using the configured
// text_to_sfx.model ("provider/model"), writing MP3 audio to outPath. A
// non-nil seconds caps the clip length; a non-nil loop requests a seamless
// loop. Errors name the configured model so a gateway without the endpoint
// (or a provider that rejects it) is diagnosable.
func (s *SFXService) Generate(ctx context.Context, prompt, outPath string, seconds *float32, loop *bool) error {
	model := s.config.TextToSFX.ResolveGatewayModel()
	provider, modelName, ok := strings.Cut(model, "/")
	if !ok || provider == "" || modelName == "" {
		return fmt.Errorf("invalid text_to_sfx.model %q (expected 'provider/model')", model)
	}

	format := sdk.CreateSFXRequestResponseFormatMp3
	request := sdk.CreateSFXRequest{
		Prompt:          prompt,
		Model:           modelName,
		ResponseFormat:  &format,
		DurationSeconds: seconds,
		Loop:            loop,
	}

	audio, err := s.client.CreateSFX(ctx, sdk.Provider(provider), request)
	if err != nil {
		return fmt.Errorf("sfx generation with %s failed: %w", model, err)
	}
	if len(audio) == 0 {
		return fmt.Errorf("sfx generation with %s returned no audio", model)
	}

	if err := os.WriteFile(outPath, audio, 0o644); err != nil { // nolint:gosec
		return fmt.Errorf("writing audio to %q: %w", outPath, err)
	}
	return nil
}
