package infrastructure

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
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
	return NewVideoService(cfg, client, nil)
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
		svc := NewVideoService(cfg, client, nil)
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

	t.Run("a local avatar model is sent whole and names no provider", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateVideoReturns(&sdk.VideoJob{ID: "local:job-1", Status: sdk.VideoJobStatusQueued}, nil)
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "local:job-1", Status: sdk.VideoJobStatusCompleted}, nil)
		client.DownloadVideoContentReturns([]byte("mp4"), nil)

		cfg := config.DefaultConfig()
		cfg.TextToVideo.AvatarModel = "local/musetalk"
		svc := NewVideoService(cfg, client, nil)

		dir := t.TempDir()
		avatar := filepath.Join(dir, "portrait.png")
		audio := filepath.Join(dir, "line.wav")
		require.NoError(t, os.WriteFile(avatar, []byte("png-bytes"), 0o600))
		require.NoError(t, os.WriteFile(audio, []byte("wav-bytes"), 0o600))

		err := svc.Render(context.Background(), agentdomain.VideoRequest{AvatarPath: avatar, AudioPath: audio}, filepath.Join(t.TempDir(), "out.mp4"))
		require.NoError(t, err)

		_, provider, req := client.CreateVideoArgsForCall(0)
		assert.Empty(t, provider, "the gateway routes a local model by its id, not by a provider")
		assert.Equal(t, "local/musetalk", req.Model)
		_, polledProvider, polledID := client.RetrieveVideoArgsForCall(0)
		assert.Empty(t, polledProvider)
		assert.Equal(t, "local:job-1", polledID)
		_, downloadProvider, _ := client.DownloadVideoContentArgsForCall(0)
		assert.Empty(t, downloadProvider)
	})

	t.Run("avatar plus audio upload builds the multipart files", func(t *testing.T) {
		client := &sdkmocks.FakeClient{}
		client.CreateVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusQueued}, nil)
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusCompleted}, nil)
		client.DownloadVideoContentReturns([]byte("mp4"), nil)

		svc := NewVideoService(config.DefaultConfig(), client, nil)

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
		svc := NewVideoService(cfg, client, nil)

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
		svc := NewVideoService(config.DefaultConfig(), client, nil)

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
		svc := NewVideoService(config.DefaultConfig(), client, nil)

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
		svc := NewVideoService(cfg, client, nil)
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
		svc := NewVideoService(cfg, client, nil)
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
				svc := NewVideoService(cfg, client, nil)

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
		svc := NewVideoService(cfg, client, nil)

		err := svc.Render(context.Background(), agentdomain.VideoRequest{Prompt: "p"}, filepath.Join(t.TempDir(), "out.mp4"))
		require.NoError(t, err)

		_, _, req := client.CreateVideoArgsForCall(0)
		require.NotNil(t, req.Size)
		assert.Equal(t, "720x1280", *req.Size)
	})
}

func TestVideoService_Dub(t *testing.T) { // nolint:funlen
	t.Run("posts the multipart dub, polls queued to completed and downloads the mp4", func(t *testing.T) {
		stub, dub := newDubStub(t, `{"id":"job-1","object":"video","model":"sfx-2.0","status":"queued"}`)
		client := &sdkmocks.FakeClient{}
		polls := 0
		client.RetrieveVideoStub = func(ctx context.Context, provider sdk.Provider, videoID string) (*sdk.VideoJob, error) {
			status := sdk.VideoJobStatusQueued
			if polls > 0 {
				status = sdk.VideoJobStatusCompleted
			}
			polls++
			return &sdk.VideoJob{ID: videoID, Status: status}, nil
		}
		client.DownloadVideoContentReturns([]byte("fake-dubbed-mp4"), nil)

		dir := t.TempDir()
		clip := filepath.Join(dir, "clip.mp4")
		require.NoError(t, os.WriteFile(clip, []byte("mp4-bytes"), 0o600))

		preserve := true
		cfg := config.DefaultConfig()
		cfg.TextToVideo.PollInterval = 1
		svc := NewVideoService(cfg, client, dub)
		outPath := filepath.Join(t.TempDir(), "out.mp4")

		err := svc.Dub(context.Background(), agentdomain.VideoDubRequest{
			Prompt:         "a busy cafe hubbub",
			VideoPath:      clip,
			PreserveSpeech: &preserve,
		}, outPath)
		require.NoError(t, err)

		form, clipData, provider, auth, posts := stub.snapshot()
		assert.Equal(t, 1, posts, "the dub create goes through the dub gateway, not the SDK")
		assert.Equal(t, "sfx-2.0", form["model"], "dubs use the default video_to_sfx.model")
		assert.Equal(t, "a busy cafe hubbub", form["prompt"])
		assert.Equal(t, "true", form["preserve_speech"])
		assert.Equal(t, []byte("mp4-bytes"), clipData)
		assert.Equal(t, "mirelo", provider, "the provider routes the create")
		assert.Equal(t, "Bearer test-key", auth)

		assert.Equal(t, 0, client.CreateVideoCallCount(), "the SDK client stays out of the dub create")
		assert.Equal(t, 2, client.RetrieveVideoCallCount(), "one poll for the queued state, one for completed")
		require.Equal(t, 1, client.DownloadVideoContentCallCount())
		_, gotProvider, gotID := client.DownloadVideoContentArgsForCall(0)
		assert.Equal(t, sdk.Provider("mirelo"), gotProvider)
		assert.Equal(t, "job-1", gotID)

		written, err := os.ReadFile(outPath)
		require.NoError(t, err)
		assert.Equal(t, []byte("fake-dubbed-mp4"), written)
	})

	t.Run("omitted knobs stay unset on the wire", func(t *testing.T) {
		stub, dub := newDubStub(t, `{"id":"job-1","status":"queued"}`)
		client := &sdkmocks.FakeClient{}
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusCompleted}, nil)
		client.DownloadVideoContentReturns([]byte("mp4"), nil)

		dir := t.TempDir()
		clip := filepath.Join(dir, "clip.webm")
		require.NoError(t, os.WriteFile(clip, []byte("webm"), 0o600))

		svc := NewVideoService(config.DefaultConfig(), client, dub)
		require.NoError(t, svc.Dub(context.Background(), agentdomain.VideoDubRequest{VideoPath: clip}, filepath.Join(t.TempDir(), "out.mp4")))

		form, clipData, _, _, _ := stub.snapshot()
		assert.Equal(t, map[string]string{"model": "sfx-2.0"}, form, "no prompt on the domain request means none on the wire")
		assert.Equal(t, []byte("webm"), clipData)
	})

	t.Run("a clip over the gateway body limit is rejected before sending", func(t *testing.T) {
		stub, dub := newDubStub(t, "{}")
		clip := filepath.Join(t.TempDir(), "clip.mp4")
		require.NoError(t, os.WriteFile(clip, make([]byte, 11<<20), 0o600))

		svc := NewVideoService(config.DefaultConfig(), &sdkmocks.FakeClient{}, dub)
		err := svc.Dub(context.Background(), agentdomain.VideoDubRequest{VideoPath: clip}, filepath.Join(t.TempDir(), "out.mp4"))
		require.ErrorContains(t, err, "10 MiB request limit")
		_, _, _, _, posts := stub.snapshot()
		assert.Equal(t, 0, posts, "an oversized clip never reaches the gateway")
	})

	t.Run("model without provider is rejected", func(t *testing.T) {
		stub, dub := newDubStub(t, "{}")
		cfg := config.DefaultConfig()
		cfg.VideoToSFX.Model = "sfx-2.0"
		svc := NewVideoService(cfg, &sdkmocks.FakeClient{}, dub)

		clip := filepath.Join(t.TempDir(), "clip.mp4")
		require.NoError(t, os.WriteFile(clip, []byte("mp4"), 0o600))

		err := svc.Dub(context.Background(), agentdomain.VideoDubRequest{VideoPath: clip}, filepath.Join(t.TempDir(), "out.mp4"))
		assert.ErrorContains(t, err, "provider/model")
		_, _, _, _, posts := stub.snapshot()
		assert.Equal(t, 0, posts)
	})

	t.Run("the job loop reuses the text_to_video timeout and poll interval", func(t *testing.T) {
		_, dub := newDubStub(t, `{"id":"job-1","status":"queued"}`)
		client := &sdkmocks.FakeClient{}
		client.RetrieveVideoReturns(&sdk.VideoJob{ID: "job-1", Status: sdk.VideoJobStatusInProgress}, nil)

		cfg := config.DefaultConfig()
		cfg.TextToVideo.Timeout = 1
		cfg.TextToVideo.PollInterval = 5
		svc := NewVideoService(cfg, client, dub)
		outPath := filepath.Join(t.TempDir(), "out.mp4")

		clip := filepath.Join(t.TempDir(), "clip.mp4")
		require.NoError(t, os.WriteFile(clip, []byte("mp4"), 0o600))

		err := svc.Dub(context.Background(), agentdomain.VideoDubRequest{VideoPath: clip}, outPath)
		assert.ErrorContains(t, err, "did not complete in time")
		assert.NoFileExists(t, outPath)
	})
}

// dubStub captures what the dub gateway posts and answers the create with a
// canned job body at POST /v1/videos.
type dubStub struct {
	mu       sync.Mutex
	form     map[string]string
	clip     []byte
	provider string
	auth     string
	posts    int
}

func newDubStub(t *testing.T, body string) (*dubStub, VideoDubGateway) {
	t.Helper()
	stub := &dubStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/videos", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		form := map[string]string{}
		for _, key := range []string{"model", "prompt", "preserve_speech"} {
			if v := r.FormValue(key); v != "" {
				form[key] = v
			}
		}
		var clip []byte
		if file, _, fileErr := r.FormFile("video"); fileErr == nil {
			defer file.Close() // nolint:errcheck
			clip, _ = io.ReadAll(file)
		}

		stub.mu.Lock()
		stub.form = form
		stub.clip = clip
		stub.provider = r.URL.Query().Get("provider")
		stub.auth = r.Header.Get("Authorization")
		stub.posts++
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return stub, NewDubGateway(ts.URL+"/v1", "test-key", ts.Client())
}

// snapshot returns what the stub captured so far.
func (s *dubStub) snapshot() (form map[string]string, clip []byte, provider, auth string, posts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.form, s.clip, s.provider, s.auth, s.posts
}
