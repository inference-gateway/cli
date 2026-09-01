package infrastructure

import (
	"context"
	"fmt"
	"os"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

// SpeechService synthesizes speech through the gateway's Audio API
// (POST /v1/audio/speech), so requests show up in gateway logs and traces.
// It implements agentdomain.SpeechService.
type SpeechService struct {
	config *config.Config
	client sdk.Client
}

// NewSpeechService creates a new gateway-backed speech service.
func NewSpeechService(cfg *config.Config, client sdk.Client) *SpeechService {
	return &SpeechService{
		config: cfg,
		client: client,
	}
}

// Synthesize generates speech for text using the configured
// text_to_speech.model ("provider/model") and voice, writing WAV audio to
// outPath. A non-empty voiceSamplePath is sent as a reference sample for
// zero-shot voice cloning on providers that support it.
func (s *SpeechService) Synthesize(ctx context.Context, text, voiceSamplePath, outPath string) error {
	model := s.config.TextToSpeech.ResolveGatewayModel()
	provider, modelName, ok := strings.Cut(model, "/")
	if !ok || provider == "" || modelName == "" {
		return fmt.Errorf("invalid text_to_speech.model %q (expected 'provider/model')", model)
	}

	format := sdk.Wav
	request := sdk.CreateSpeechRequest{
		Input:          text,
		Model:          modelName,
		Voice:          strings.TrimSpace(s.config.TextToSpeech.Voice),
		ResponseFormat: &format,
	}
	if voiceSamplePath != "" {
		sample, err := os.ReadFile(voiceSamplePath) // nolint:gosec
		if err != nil {
			return fmt.Errorf("reading voice sample %q: %w", voiceSamplePath, err)
		}
		request.ReferenceAudio = &sample
	}

	audio, err := s.client.CreateSpeech(ctx, sdk.Provider(provider), request)
	if err != nil {
		return err
	}
	if len(audio) == 0 {
		return fmt.Errorf("speech synthesis returned no audio")
	}

	if err := os.WriteFile(outPath, audio, 0o644); err != nil { // nolint:gosec
		return fmt.Errorf("writing audio to %q: %w", outPath, err)
	}
	return nil
}
