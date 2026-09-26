package infrastructure

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func newTestVideoService(model string, client sdk.Client) *VideoService {
	cfg := config.DefaultConfig()
	cfg.TextToVideo.Model = model
	return NewVideoService(cfg, client)
}

func TestVideoService_Render(t *testing.T) { // nolint:funlen
	t.Run("polls queued to completed and downloads the mp4", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusQueued}, nil)
		polls := 0
		client.RetrieveVideoStub = func(ctx context.Context, provider sdk.Provider, videoID string) (*sdk.VideoJob, error) {
			status := sdk.VideoJobStatusQueued
			if polls > 0 {
				status = sdk.VideoJobStatusCompleted
			}
			polls++
			return &sdk.VideoJob{ID: videoID, Status: status, Model: "creatify-aurora"}, nil
		}
		client.DownloadVideoContentReturns([]byte("fake-mp4-bytes"), nil)

		cfg := config.DefaultConfig()
		cfg.TextToVideo.PollInterval = 1
		svc := NewVideoService(cfg, client)
		outPath := filepath.Join(t.TempDir(), "out.mp4")

		err := svc.Render(context.Background(), agentdomain.VideoRequest{Prompt: "a neon city flyover", Seconds: "4", Size: "720x1280"}, outPath)
		require.NoError(t, err)

		require.Equal(t, 1, client.CreateVideoCallCount())
		_, provider, req := client.CreateVideoArgsForCall(0)
		assert.Equal(t, sdk.Provider("elevenlabs"), provider)
		assert.Equal(t, "veo-3.1-fast-generate-001", req.Model, "prompt renders use the default text_to_video.model")
		require.NotNil(t, req.Prompt)
		assert.Equal(t, "a neon city flyover", *req.Prompt)
		require.NotNil(t, req.Seconds)
		assert.Equal(t, "4", *req.Seconds)
		require.NotNil(t, req.Size)
		assert.Equal(t, "720x1280", *req.Size)
		assert.Nil(t, req.InputReference)
		assert.Nil(t, req.Audio)

		assert.Equal(t, 2, client.RetrieveVideoCallCount(), "one poll for the queued state, one for completed")
		assert.Equal(t, 2, polls)

		require.Equal(t, 1, client.DownloadVideoContentCallCount())
		_, gotProvider, gotID := client.DownloadVideoContentArgsForCall(0)
		assert.Equal(t, sdk.Provider("elevenlabs"), gotProvider)
		assert.Equal(t, "job-1", gotID)

		written, err := os.ReadFile(outPath)
		require.NoError(t, err)
		assert.Equal(t, []byte("fake-mp4-bytes"), written)
	})

	t.Run("avatar plus audio upload builds the multipart files", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusQueued}, nil)
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusCompleted}, nil)
		client.DownloadVideoContentReturns([]byte("mp4"), nil)

		svc := NewVideoService(config.DefaultConfig(), client)

		dir := t.TempDir()
		avatar := filepath.Join(dir, "portrait.png")
		audio := filepath.Join(dir, "line.wav")
		require.NoError(t, os.WriteFile(avatar, []byte("png-bytes"), 0o600))
		require.NoError(t, os.WriteFile(audio, []byte("wav-bytes"), 0o600))

		err := svc.Render(context.Background(), agentdomain.VideoRequest{
			Prompt:     "close up, soft light",
			AvatarPath: avatar,
			AudioPath:  audio,
		}, filepath.Join(t.TempDir(), "out.mp4"))
		require.NoError(t, err)

		_, _, req := client.CreateVideoArgsForCall(0)
		assert.Equal(t, "creatify-aurora", req.Model, "avatar renders use the default text_to_video.avatar_model")
		require.NotNil(t, req.InputReference)
		assert.Equal(t, "portrait.png", req.InputReference.Filename())
		avatarBytes, err := req.InputReference.Bytes()
		require.NoError(t, err)
		assert.Equal(t, []byte("png-bytes"), avatarBytes)

		require.NotNil(t, req.Audio)
		assert.Equal(t, "line.wav", req.Audio.Filename())
		audioBytes, err := req.Audio.Bytes()
		require.NoError(t, err)
		assert.Equal(t, []byte("wav-bytes"), audioBytes)
	})

	t.Run("combined upload over the gateway body limit is rejected before sending", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		cfg := config.DefaultConfig()
		cfg.TextToVideo.Model = "elevenlabs/creatify-aurora"
		svc := NewVideoService(cfg, client)

		dir := t.TempDir()
		avatar := filepath.Join(dir, "portrait.png")
		audio := filepath.Join(dir, "line.wav")
		require.NoError(t, os.WriteFile(avatar, make([]byte, 6<<20), 0o600))
		require.NoError(t, os.WriteFile(audio, make([]byte, 5<<20), 0o600))

		err := svc.Render(context.Background(), agentdomain.VideoRequest{AvatarPath: avatar, AudioPath: audio}, filepath.Join(t.TempDir(), "out.mp4"))
		require.ErrorContains(t, err, "10 MiB request limit")
		assert.Equal(t, 0, client.CreateVideoCallCount())
	})

	t.Run("reference images go as repeated parts with the prompt model", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusQueued}, nil)
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusCompleted}, nil)
		client.DownloadVideoContentReturns([]byte("mp4"), nil)
		svc := NewVideoService(config.DefaultConfig(), client)

		dir := t.TempDir()
		front := filepath.Join(dir, "01-front.jpg")
		left := filepath.Join(dir, "02-left.png")
		require.NoError(t, os.WriteFile(front, []byte("front"), 0o600))
		require.NoError(t, os.WriteFile(left, []byte("left"), 0o600))

		err := svc.Render(context.Background(), agentdomain.VideoRequest{
			Prompt:         "the presenter waves",
			ReferencePaths: []string{front, left},
		}, filepath.Join(t.TempDir(), "out.mp4"))
		require.NoError(t, err)

		_, _, req := client.CreateVideoArgsForCall(0)
		assert.Equal(t, "veo-3.1-fast-generate-001", req.Model)
		assert.Nil(t, req.InputReference)
		require.NotNil(t, req.ReferenceImages)
		require.Len(t, *req.ReferenceImages, 2)
		for i, want := range []string{"front", "left"} {
			data, err := (*req.ReferenceImages)[i].Bytes()
			require.NoError(t, err)
			assert.Equal(t, want, string(data))
		}
		assert.Equal(t, "01-front.jpg", (*req.ReferenceImages)[0].Filename())
	})

	t.Run("reference images count toward the body limit", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		svc := NewVideoService(config.DefaultConfig(), client)

		dir := t.TempDir()
		var refs []string
		for _, name := range []string{"a.png", "b.png", "c.png"} {
			path := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(path, make([]byte, 4<<20), 0o600))
			refs = append(refs, path)
		}

		err := svc.Render(context.Background(), agentdomain.VideoRequest{Prompt: "wave", ReferencePaths: refs}, filepath.Join(t.TempDir(), "out.mp4"))
		require.ErrorContains(t, err, "10 MiB request limit")
		assert.Equal(t, 0, client.CreateVideoCallCount())
	})

	t.Run("failed job surfaces the provider message and names the model", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		message := "content policy violation"
		client.CreateVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusQueued}, nil)
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusFailed, Error: &struct {
			Code    *string `json:"code,omitempty"`
			Message *string `json:"message,omitempty"`
		}{Message: &message}}, nil)

		cfg := config.DefaultConfig()
		cfg.TextToVideo.Model = "elevenlabs/creatify-aurora"
		svc := NewVideoService(cfg, client)
		outPath := filepath.Join(t.TempDir(), "out.mp4")

		err := svc.Render(context.Background(), agentdomain.VideoRequest{Prompt: "p"}, outPath)
		assert.ErrorContains(t, err, "video generation with elevenlabs/creatify-aurora failed")
		assert.ErrorContains(t, err, "content policy violation")
		assert.NoFileExists(t, outPath)
		assert.Equal(t, 0, client.DownloadVideoContentCallCount())
	})

	t.Run("timeout ends the poll loop without a partial file", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusQueued}, nil)
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusInProgress}, nil)

		cfg := config.DefaultConfig()
		cfg.TextToVideo.Model = "elevenlabs/creatify-aurora"
		cfg.TextToVideo.Timeout = 1
		cfg.TextToVideo.PollInterval = 5
		svc := NewVideoService(cfg, client)
		outPath := filepath.Join(t.TempDir(), "out.mp4")

		err := svc.Render(context.Background(), agentdomain.VideoRequest{Prompt: "p"}, outPath)
		assert.ErrorContains(t, err, "did not complete in time")
		assert.NoFileExists(t, outPath)
	})

	t.Run("model without provider is rejected", func(t *testing.T) {
		svc := newTestVideoService("creatify-aurora", &sdkmocks.FakeClient{})
		err := svc.Render(context.Background(), agentdomain.VideoRequest{Prompt: "p"}, filepath.Join(t.TempDir(), "out.mp4"))
		assert.ErrorContains(t, err, "provider/model")
	})

	t.Run("model and avatar_model swap independently", func(t *testing.T) {
		dir := t.TempDir()
		avatar := filepath.Join(dir, "portrait.png")
		audio := filepath.Join(dir, "line.mp3")
		require.NoError(t, os.WriteFile(avatar, []byte("png"), 0o600))
		require.NoError(t, os.WriteFile(audio, []byte("mp3"), 0o600))

		tests := []struct {
			name         string
			request      agentdomain.VideoRequest
			wantProvider sdk.Provider
			wantModel    string
		}{
			{"prompt", agentdomain.VideoRequest{Prompt: "p"}, "elevenlabs", "bytedance-seedance-v2-fast"},
			{"first frame", agentdomain.VideoRequest{Prompt: "p", AvatarPath: avatar}, "elevenlabs", "bytedance-seedance-v2-fast"},
			{"avatar", agentdomain.VideoRequest{AvatarPath: avatar, AudioPath: audio}, "acme", "talking-head-2"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := &sdkmocks.FakeClient{}
				client.CreateVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusQueued}, nil)
				client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusCompleted}, nil)
				client.DownloadVideoContentReturns([]byte("mp4"), nil)

				cfg := config.DefaultConfig()
				cfg.TextToVideo.Model = "elevenlabs/bytedance-seedance-v2-fast"
				cfg.TextToVideo.AvatarModel = "acme/talking-head-2"
				svc := NewVideoService(cfg, client)

				require.NoError(t, svc.Render(context.Background(), tt.request, filepath.Join(t.TempDir(), "out.mp4")))
				_, provider, req := client.CreateVideoArgsForCall(0)
				assert.Equal(t, tt.wantProvider, provider)
				assert.Equal(t, tt.wantModel, req.Model)
			})
		}
	})

	t.Run("config size is the fallback for an unsupplied size", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusQueued}, nil)
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusCompleted}, nil)
		client.DownloadVideoContentReturns([]byte("mp4"), nil)

		cfg := config.DefaultConfig()
		cfg.TextToVideo.Model = "elevenlabs/creatify-aurora"
		cfg.TextToVideo.Size = "720x1280"
		svc := NewVideoService(cfg, client)

		err := svc.Render(context.Background(), agentdomain.VideoRequest{Prompt: "p"}, filepath.Join(t.TempDir(), "out.mp4"))
		require.NoError(t, err)

		_, _, req := client.CreateVideoArgsForCall(0)
		require.NotNil(t, req.Size)
		assert.Equal(t, "720x1280", *req.Size)
	})
}
