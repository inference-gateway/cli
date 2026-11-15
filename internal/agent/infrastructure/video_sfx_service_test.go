package infrastructure

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// newScoredGateway starts a stand-in /v1/audio/sfx gateway. The handler
// records the multipart form the service posts and answers with status,
// contentType and audio, so the CLI-side contract is pinned end to end.
func newScoredGateway(t *testing.T, status int, contentType, audio string, mutate func(*config.Config)) (*VideoSFXService, *sync.Mutex, *sfxFormState) {
	t.Helper()

	state := &sfxFormState{fields: map[string]string{}}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, "/v1/audio/sfx", r.URL.Path)
		state.provider = r.URL.Query().Get("provider")
		state.auth = r.Header.Get("Authorization")
		require.NoError(t, r.ParseMultipartForm(1<<20))
		for _, name := range []string{"model", "prompt", "duration_seconds", "start_offset_seconds", "preserve_speech", "output"} {
			if v := r.FormValue(name); v != "" {
				state.fields[name] = v
			}
		}
		file, header, err := r.FormFile("video")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()
		state.video = header.Filename
		content, err := io.ReadAll(file)
		require.NoError(t, err)
		state.videoLen = int64(len(content))
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(audio))
	}))
	t.Cleanup(server.Close)

	cfg := config.DefaultConfig()
	if mutate != nil {
		mutate(cfg)
	}
	svc := NewVideoSFXService(cfg, GatewayEndpoint{Client: server.Client(), URL: server.URL + "/v1", APIKey: "gateway-key"})
	return svc, &mu, state
}

// sfxFormState captures the /v1/audio/sfx multipart form one Score call sent.
type sfxFormState struct {
	provider string
	fields   map[string]string
	video    string
	videoLen int64
	auth     string
}

func TestVideoSFXServiceScore(t *testing.T) {
	clip := filepath.Join(t.TempDir(), "clip.mov")
	require.NoError(t, os.WriteFile(clip, []byte("fake-video-bytes"), 0o600))

	seconds, start := float32(3), float32(1.5)
	preserve := true
	request := agentdomain.VideoSoundEffectRequest{
		VideoPath:      clip,
		Prompt:         "soft keyboard taps",
		Seconds:        &seconds,
		StartSeconds:   &start,
		Track:          "speech",
		PreserveSpeech: &preserve,
	}

	t.Run("posts the unprefixed model, the knobs, the clip and the provider query", func(t *testing.T) {
		svc, mu, state := newScoredGateway(t, http.StatusOK, "audio/wav", "scored-speech-bytes", nil)
		outPath := filepath.Join(t.TempDir(), "sfx-000000-000000-c1.mp3")

		saved, err := svc.Score(context.Background(), request, outPath)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "mirelo", state.provider)
		assert.Equal(t, "Bearer gateway-key", state.auth)
		assert.Equal(t, "sfx-2.0", state.fields["model"])
		assert.Equal(t, "soft keyboard taps", state.fields["prompt"])
		assert.Equal(t, "3", state.fields["duration_seconds"])
		assert.Equal(t, "1.5", state.fields["start_offset_seconds"])
		assert.Equal(t, "true", state.fields["preserve_speech"])
		assert.Equal(t, "speech", state.fields["output"])
		assert.Equal(t, "clip.mov", state.video)
		assert.Equal(t, int64(len("fake-video-bytes")), state.videoLen)

		written, err := os.ReadFile(saved)
		require.NoError(t, err)
		assert.Equal(t, []byte("scored-speech-bytes"), written)
		assert.Equal(t, ".wav", filepath.Ext(saved), "the extension follows the response content type")
		assert.NoFileExists(t, outPath, "the resolved placeholder under the old extension does not survive")
	})

	t.Run("an omitted knob is not sent and an mp3 answer keeps the resolved name", func(t *testing.T) {
		svc, mu, state := newScoredGateway(t, http.StatusOK, "audio/mpeg", "scored-mix-bytes", nil)
		outPath := filepath.Join(t.TempDir(), "sfx-000000-000000-c2.mp3")

		saved, err := svc.Score(context.Background(), agentdomain.VideoSoundEffectRequest{VideoPath: clip}, outPath)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "sfx-2.0", state.fields["model"])
		assert.NotContains(t, state.fields, "prompt")
		assert.NotContains(t, state.fields, "duration_seconds")
		assert.NotContains(t, state.fields, "start_offset_seconds")
		assert.NotContains(t, state.fields, "preserve_speech")
		assert.NotContains(t, state.fields, "output")

		written, err := os.ReadFile(saved)
		require.NoError(t, err)
		assert.Equal(t, []byte("scored-mix-bytes"), written)
		assert.Equal(t, outPath, saved, "the answer is already mp3")
	})

	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name      string
			model     string
			status    int
			body      string
			noVideo   bool
			wantParts []string
		}{
			{
				name:      "model without provider",
				model:     "sfx-2.0",
				wantParts: []string{"invalid video_to_sfx.model", "provider/model"},
			},
			{
				name:      "missing video file",
				noVideo:   true,
				wantParts: []string{"reading video"},
			},
			{
				name:      "the gateway's error passes through",
				status:    http.StatusBadRequest,
				body:      `{"error":"Video scoring is not supported"}`,
				wantParts: []string{"sfx scoring with mirelo/sfx-2.0 failed", "Video scoring is not supported"},
			},
			{
				name:      "a nested provider error passes through",
				status:    http.StatusBadGateway,
				body:      `{"error":{"code":"moderation_blocked","message":"the prompt was blocked"}}`,
				wantParts: []string{"the prompt was blocked"},
			},
			{
				name:      "a 413 names the gateway's body limit",
				status:    http.StatusRequestEntityTooLarge,
				body:      `{"error":"the clip is too large"}`,
				wantParts: []string{"SERVER_MAX_REQUEST_BODY_SIZE", "the clip is too large"},
			},
			{
				name:      "an empty answer is an error",
				status:    http.StatusOK,
				wantParts: []string{"returned no audio"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var request agentdomain.VideoSoundEffectRequest
				request.VideoPath = clip
				if tt.noVideo {
					request.VideoPath = filepath.Join(t.TempDir(), "missing.mov")
				}
				outPath := filepath.Join(t.TempDir(), "out.mp3")
				svc, _, _ := newScoredGateway(t, tt.status, "audio/mpeg", tt.body, func(cfg *config.Config) {
					cfg.VideoToSFX.Model = tt.model
				})

				saved, err := svc.Score(context.Background(), request, outPath)

				require.Error(t, err)
				for _, want := range tt.wantParts {
					assert.Contains(t, err.Error(), want)
				}
				assert.Equal(t, outPath, saved, "a failed call resolves nothing new")
				assert.NoFileExists(t, outPath)
			})
		}
	})
}

func TestVideoSFXServiceScoreGatewayDown(t *testing.T) {
	clip := filepath.Join(t.TempDir(), "clip.mov")
	require.NoError(t, os.WriteFile(clip, []byte("fake-video-bytes"), 0o600))
	svc := NewVideoSFXService(config.DefaultConfig(), GatewayEndpoint{
		Client: &http.Client{},
		URL:    "http://127.0.0.1:0/v1",
	})
	outPath := filepath.Join(t.TempDir(), "out.mp3")

	_, err := svc.Score(context.Background(), agentdomain.VideoSoundEffectRequest{VideoPath: clip}, outPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sfx scoring with mirelo/sfx-2.0 failed")
	assert.True(t, errors.Is(err, io.EOF) || strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "no such host"), err.Error())
}
