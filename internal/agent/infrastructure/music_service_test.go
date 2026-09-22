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

func newTestMusicService(model string, client sdk.Client) *MusicService {
	cfg := config.DefaultConfig()
	cfg.TextToMusic.Model = model
	return NewMusicService(cfg, client)
}

func TestMusicService_Compose(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateMusicReturns([]byte("fake-wav-bytes"), nil)
	svc := newTestMusicService("elevenlabs/music_v2_5", client)
	outPath := filepath.Join(t.TempDir(), "out.wav")

	seconds := float32(30)
	instrumental := true
	err := svc.Compose(context.Background(), "calm piano loop", outPath, &seconds, &instrumental)
	require.NoError(t, err)

	require.Equal(t, 1, client.CreateMusicCallCount())
	_, provider, req := client.CreateMusicArgsForCall(0)
	assert.Equal(t, sdk.Provider("elevenlabs"), provider)
	assert.Equal(t, "music_v2_5", req.Model)
	assert.Equal(t, "calm piano loop", req.Prompt)
	require.NotNil(t, req.DurationSeconds)
	assert.InDelta(t, 30.0, *req.DurationSeconds, 0.01)
	require.NotNil(t, req.Instrumental)
	assert.True(t, *req.Instrumental)
	require.NotNil(t, req.ResponseFormat)
	assert.Equal(t, sdk.CreateMusicRequestResponseFormatWav, *req.ResponseFormat)

	written, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("fake-wav-bytes"), written)
}

func TestMusicService_ComposeDefaultModel(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateMusicReturns([]byte("wav"), nil)
	svc := newTestMusicService("", client)
	outPath := filepath.Join(t.TempDir(), "out.wav")

	require.NoError(t, svc.Compose(context.Background(), "hi", outPath, nil, nil))

	_, provider, req := client.CreateMusicArgsForCall(0)
	assert.Equal(t, sdk.Provider("elevenlabs"), provider)
	assert.Equal(t, "music_v2_5", req.Model)
	assert.Nil(t, req.DurationSeconds)
	assert.Nil(t, req.Instrumental)
}

func TestMusicService_ComposeErrors(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.wav")

	t.Run("model without provider", func(t *testing.T) {
		svc := newTestMusicService("music_v2_5", &sdkmocks.FakeClient{})
		err := svc.Compose(context.Background(), "hi", outPath, nil, nil)
		assert.ErrorContains(t, err, "provider/model")
	})

	t.Run("gateway error names the model", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateMusicReturns(nil, fmt.Errorf("provider does not support music"))
		svc := newTestMusicService("elevenlabs/music_v2_5", client)
		err := svc.Compose(context.Background(), "hi", outPath, nil, nil)
		assert.ErrorContains(t, err, "music generation with elevenlabs/music_v2_5 failed")
		assert.ErrorContains(t, err, "does not support music")
		assert.NoFileExists(t, outPath)
	})

	t.Run("empty audio is an error", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateMusicReturns([]byte{}, nil)
		svc := newTestMusicService("elevenlabs/music_v2_5", client)
		err := svc.Compose(context.Background(), "hi", outPath, nil, nil)
		assert.ErrorContains(t, err, "no audio")
		assert.NoFileExists(t, outPath)
	})
}
