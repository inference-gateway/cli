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

// TestDubVideoToolAgainstMockGateway drives the full loop: the chat model
// requests the DubVideo tool, the tool creates the dubbing job on /v1/videos
// with the clip as its video part and polls it, and the dubbed MP4 - the clip
// re-rendered with the generated sound as its soundtrack - is written into
// text_to_video.output_dir. The pinned tokenless does not serve dubbing, so
// the three video routes run on a tiny in-test mock layered over the
// tokenless chat gateway, following the same job lifecycle (queued to
// completed after two polls).
func TestDubVideoToolAgainstMockGateway(t *testing.T) { // nolint:funlen
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: dub-clip
    match: '(?i)dub this clip'
    turns:
      - tool_calls:
          - { name: DubVideo, args: { video: "clip.mp4", prompt: "a busy cafe hubbub", preserve_speech: true } }
      - content: "Dubbed."
`))
	require.NoError(t, err)
	std, err := mockgateway.LoadFile(filepath.Join(repoRoot(), "tests", "integration", "scenarios.yaml"))
	require.NoError(t, err)
	defs.Models = std.Models

	var mu sync.Mutex
	var createdForm map[string]string
	var uploadedClip []byte
	polls := 0

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/videos", func(w http.ResponseWriter, r *http.Request) {
		form := map[string]string{}
		_ = r.ParseMultipartForm(1 << 20)
		for _, key := range []string{"model", "prompt", "preserve_speech"} {
			if v := r.FormValue(key); v != "" {
				form[key] = v
			}
		}
		if file, _, fileErr := r.FormFile("video"); fileErr == nil {
			defer file.Close() // nolint:errcheck
			data, _ := io.ReadAll(file)
			mu.Lock()
			uploadedClip = data
			mu.Unlock()
		}
		mu.Lock()
		createdForm = form
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"job-1","object":"video","created_at":1,"model":"sfx-2.0","status":"queued"}`)
	})
	mux.HandleFunc("GET /v1/videos/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		polls++
		status := "queued"
		if polls >= 2 {
			status = "completed"
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"job-1","object":"video","created_at":1,"model":"sfx-2.0","status":%q,"progress":100}`, status)
	})
	mux.HandleFunc("GET /v1/videos/{id}/content", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = fmt.Fprint(w, "fake-mp4-bytes")
	})
	mux.Handle("/", mockgateway.New(defs))

	outDir := ""
	e := newEnvWithHandler(t, mux, func(cfg *config.Config) {
		cfg.VideoToSFX.Enabled = true
		cfg.VideoToSFX.Dub = true
		cfg.TextToVideo.OutputDir = filepath.Join(t.TempDir(), "dub-out")
		cfg.TextToVideo.PollInterval = 1
		outDir = cfg.TextToVideo.OutputDir
	})
	require.NoError(t, os.WriteFile("clip.mp4", []byte("mp4-bytes"), 0o600), "the clip fixture goes into the working directory the env chdir'd into")

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	req := &agentdomain.AgentRequest{
		RequestID: "req-dub-video",
		Model:     testModel,
		Messages:  []sdk.Message{userMessage(t, "dub this clip")},
	}
	events, err := e.container.GetAgentService().RunWithStream(ctx, req)
	require.NoError(t, err)

	var (
		content    string
		dubResult  *agentdomain.ToolExecutionResult
		completed  bool
		dubbedOnce bool
		errs       []error
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
					if res.ToolName == tools.ToolDubVideo && !dubbedOnce {
						dubbedOnce = true
						dubResult = res
					}
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for the agent event channel to close")
		}
	}

	require.Empty(t, errs)
	require.True(t, completed)
	require.Contains(t, content, "Dubbed.")

	require.NotNil(t, dubResult, "expected a DubVideo tool result")
	res := dubResult
	require.True(t, res.Success, res.Error)
	data, ok := res.Data.(map[string]any)
	require.True(t, ok, "tool result data must be a map, got %T", res.Data)
	require.Equal(t, config.VideoToSFXGatewayDefaultModel, data["model"])
	require.Equal(t, true, data["preserve_speech"])

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "sfx-2.0", createdForm["model"], "the provider part of video_to_sfx.model goes on the form")
	require.Equal(t, "a busy cafe hubbub", createdForm["prompt"])
	require.Equal(t, "true", createdForm["preserve_speech"])
	require.Equal(t, []byte("mp4-bytes"), uploadedClip, "the clip rides the multipart body as its video part")
	require.Equal(t, 2, polls, "one poll for the queued state, one for completed")

	entries, err := filepath.Glob(filepath.Join(outDir, "*.mp4"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one dubbed clip")
	require.Equal(t, entries[0], data["path"])
}
