package infrastructure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

func newTestSpeechService(model, voice string, client sdk.Client) *SpeechService {
	cfg := config.DefaultConfig()
	cfg.TextToSpeech.Engine = config.TextToSpeechEngineGateway
	cfg.TextToSpeech.Model = model
	cfg.TextToSpeech.Voice = voice
	return NewSpeechService(cfg, client)
}

func TestSpeechService_Synthesize(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateSpeechReturns([]byte("fake-wav-bytes"), nil)
	svc := newTestSpeechService("openai/gpt-4o-mini-tts", "alloy", client)
	outPath := filepath.Join(t.TempDir(), "out.wav")

	err := svc.Synthesize(context.Background(), "hello world", "", outPath)
	require.NoError(t, err)

	require.Equal(t, 1, client.CreateSpeechCallCount())
	_, provider, req := client.CreateSpeechArgsForCall(0)
	assert.Equal(t, sdk.Provider("openai"), provider)
	assert.Equal(t, "gpt-4o-mini-tts", req.Model)
	assert.Equal(t, "hello world", req.Input)
	assert.Equal(t, "alloy", req.Voice)
	require.NotNil(t, req.ResponseFormat)
	assert.Equal(t, sdk.Wav, *req.ResponseFormat)
	assert.Nil(t, req.ReferenceAudio)

	written, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("fake-wav-bytes"), written)
}

func TestSpeechService_SynthesizeVoiceClone(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateSpeechReturns([]byte("cloned"), nil)
	svc := newTestSpeechService("deepinfra/qwen3-tts", "", client)

	samplePath := filepath.Join(t.TempDir(), "sample.wav")
	require.NoError(t, os.WriteFile(samplePath, []byte("RIFF-sample"), 0o644))
	outPath := filepath.Join(t.TempDir(), "out.wav")

	err := svc.Synthesize(context.Background(), "clone me", samplePath, outPath)
	require.NoError(t, err)

	_, _, req := client.CreateSpeechArgsForCall(0)
	require.NotNil(t, req.ReferenceAudio)
	assert.Equal(t, []byte("RIFF-sample"), *req.ReferenceAudio)
}

func TestSpeechService_SynthesizeErrors(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.wav")

	t.Run("model without provider", func(t *testing.T) {
		svc := newTestSpeechService("gpt-4o-mini-tts", "alloy", &sdkmocks.FakeClient{})
		err := svc.Synthesize(context.Background(), "hi", "", outPath)
		assert.ErrorContains(t, err, "provider/model")
	})

	t.Run("unreadable voice sample", func(t *testing.T) {
		svc := newTestSpeechService("openai/tts-1", "alloy", &sdkmocks.FakeClient{})
		err := svc.Synthesize(context.Background(), "hi", filepath.Join(t.TempDir(), "missing.wav"), outPath)
		assert.ErrorContains(t, err, "voice sample")
	})

	t.Run("gateway error is propagated", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateSpeechReturns(nil, fmt.Errorf("provider does not support speech"))
		svc := newTestSpeechService("openai/tts-1", "alloy", client)
		err := svc.Synthesize(context.Background(), "hi", "", outPath)
		assert.ErrorContains(t, err, "does not support speech")
		assert.NoFileExists(t, outPath)
	})

	t.Run("empty audio is an error", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateSpeechReturns([]byte{}, nil)
		svc := newTestSpeechService("openai/tts-1", "alloy", client)
		err := svc.Synthesize(context.Background(), "hi", "", outPath)
		assert.ErrorContains(t, err, "no audio")
	})
}
