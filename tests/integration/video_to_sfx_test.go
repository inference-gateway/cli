package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"
	mockgateway "github.com/inference-gateway/tokenless/gateway"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// TestVideoToSFXToolAgainstMockGateway drives the full loop: the chat model
// requests VideoToSFX, the tool posts the clip as the multipart video form and
// the answered track lands in video_to_sfx.output_dir. The multipart route
// runs on an in-test handler layered over the tokenless chat gateway, because
// the pinned tokenless serves /v1/audio/sfx for a JSON body only.
func TestVideoToSFXToolAgainstMockGateway(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: video-scored
    match: '(?i)score this video'
    turns:
      - tool_calls:
          - { name: VideoToSFX, args: { video: "clip.mp4", prompt: "soft keyboard taps", seconds: 3, start_seconds: 1, track: "speech", preserve_speech: true } }
      - content: "Sound effect scored."
`))
	require.NoError(t, err)
	std, err := mockgateway.LoadFile(filepath.Join(repoRoot(), "tests", "integration", "scenarios.yaml"))
	require.NoError(t, err)
	defs.Models = std.Models

	var mu sync.Mutex
	var provider, formModel, videoName string
	var videoLen int64

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/audio/sfx", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		provider = r.URL.Query().Get("provider")
		_ = r.ParseMultipartForm(1 << 20)
		formModel = r.FormValue("model")
		file, header, err := r.FormFile("video")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()
		videoName = header.Filename
		content, readErr := io.ReadAll(file)
		require.NoError(t, readErr)
		videoLen = int64(len(content))
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = fmt.Fprint(w, "canned-speech-bytes")
	})
	mux.Handle("/", mockgateway.New(defs))

	sfxDir := ""
	e := newEnvWithHandler(t, mux, func(cfg *config.Config) {
		cfg.VideoToSFX.Enabled = true
		cfg.VideoToSFX.OutputDir = filepath.Join(t.TempDir(), "sfx-out")
		sfxDir = cfg.VideoToSFX.OutputDir
	})
	require.NoError(t, os.WriteFile("clip.mp4", []byte("fake-video-bytes"), 0o600), "the clip must sit in the working directory the tool resolves names from")

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	req := &agentdomain.AgentRequest{
		RequestID: "req-video-to-sfx",
		Model:     testModel,
		Messages:  []sdk.Message{userMessage(t, "score this video")},
	}
	events, err := e.container.GetAgentService().RunWithStream(ctx, req)
	require.NoError(t, err)

	var (
		content   string
		scored    *agentdomain.ToolExecutionResult
		completed bool
		errs      []error
	)
	deadline := time.After(runTimeout)
loop:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break loop
			}
			switch ev := ev.(type) {
			case agentdomain.ChatChunkEvent:
				content += ev.Content
			case agentdomain.ChatCompleteEvent:
				completed = true
			case agentdomain.ChatErrorEvent:
				errs = append(errs, ev.Error)
			case agentdomain.ToolExecutionCompletedEvent:
				for _, res := range ev.Results {
					if res.ToolName == tools.ToolVideoToSFX {
						scored = res
					}
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for the agent event channel to close")
		}
	}

	require.Empty(t, errs)
	require.True(t, completed)
	require.Contains(t, content, "Sound effect scored.")

	require.NotNil(t, scored, "expected a VideoToSFX tool result")
	res := scored
	require.True(t, res.Success, res.Error)
	data, ok := res.Data.(map[string]any)
	require.True(t, ok, "tool result data must be a map, got %T", res.Data)
	require.Equal(t, "clip.mp4", data["video"])
	require.Equal(t, "soft keyboard taps", data["prompt"])
	require.Equal(t, "speech", data["track"])

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "mirelo", provider, "the provider rides in the query parameter")
	require.Equal(t, "sfx-2.0", formModel, "the model field carries no provider prefix")
	require.Equal(t, "clip.mp4", videoName)
	require.Equal(t, int64(len("fake-video-bytes")), videoLen)

	entries, err := os.ReadDir(sfxDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one saved track")
	require.Equal(t, ".wav", filepath.Ext(entries[0].Name()), "the extension follows the response content type")
	require.Equal(t, filepath.Join(sfxDir, entries[0].Name()), data["path"])
}
