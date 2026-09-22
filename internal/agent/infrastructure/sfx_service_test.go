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

func newTestSFXService(model string, client sdk.Client) *SFXService {
	cfg := config.DefaultConfig()
	cfg.TextToSFX.Model = model
	return NewSFXService(cfg, client)
}

func TestSFXService_Generate(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateSFXReturns([]byte("fake-mp3-bytes"), nil)
	svc := newTestSFXService("elevenlabs/eleven_text_to_sound_v2", client)
	outPath := filepath.Join(t.TempDir(), "out.mp3")

	seconds := float32(2.5)
	loop := true
	err := svc.Generate(context.Background(), "distant thunder rolling over a valley", outPath, &seconds, &loop)
	require.NoError(t, err)

	require.Equal(t, 1, client.CreateSFXCallCount())
	_, provider, req := client.CreateSFXArgsForCall(0)
	assert.Equal(t, sdk.Provider("elevenlabs"), provider)
	assert.Equal(t, "eleven_text_to_sound_v2", req.Model)
	assert.Equal(t, "distant thunder rolling over a valley", req.Prompt)
	require.NotNil(t, req.DurationSeconds)
	assert.InDelta(t, 2.5, *req.DurationSeconds, 0.01)
	require.NotNil(t, req.Loop)
	assert.True(t, *req.Loop)
	require.NotNil(t, req.ResponseFormat)
	assert.Equal(t, sdk.CreateSFXRequestResponseFormatMp3, *req.ResponseFormat)

	written, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("fake-mp3-bytes"), written)
}

func TestSFXService_GenerateDefaultModel(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateSFXReturns([]byte("mp3"), nil)
	svc := newTestSFXService("", client)
	outPath := filepath.Join(t.TempDir(), "out.mp3")

	require.NoError(t, svc.Generate(context.Background(), "hi", outPath, nil, nil))

	_, provider, req := client.CreateSFXArgsForCall(0)
	assert.Equal(t, sdk.Provider("elevenlabs"), provider)
	assert.Equal(t, "eleven_text_to_sound_v2", req.Model)
	assert.Nil(t, req.DurationSeconds)
	assert.Nil(t, req.Loop)
}

func TestSFXService_GenerateErrors(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.mp3")

	t.Run("model without provider", func(t *testing.T) {
		svc := newTestSFXService("eleven_text_to_sound_v2", &sdkmocks.FakeClient{})
		err := svc.Generate(context.Background(), "hi", outPath, nil, nil)
		assert.ErrorContains(t, err, "provider/model")
	})

	t.Run("gateway error names the model", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateSFXReturns(nil, fmt.Errorf("provider does not support sfx"))
		svc := newTestSFXService("elevenlabs/eleven_text_to_sound_v2", client)
		err := svc.Generate(context.Background(), "hi", outPath, nil, nil)
		assert.ErrorContains(t, err, "sfx generation with elevenlabs/eleven_text_to_sound_v2 failed")
		assert.ErrorContains(t, err, "does not support sfx")
		assert.NoFileExists(t, outPath)
	})

	t.Run("empty audio is an error", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateSFXReturns([]byte{}, nil)
		svc := newTestSFXService("elevenlabs/eleven_text_to_sound_v2", client)
		err := svc.Generate(context.Background(), "hi", outPath, nil, nil)
		assert.ErrorContains(t, err, "no audio")
		assert.NoFileExists(t, outPath)
	})
}
