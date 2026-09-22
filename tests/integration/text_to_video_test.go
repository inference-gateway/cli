package integration

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"
	mockgateway "github.com/inference-gateway/tokenless/gateway"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TestTextToVideoToolAgainstMockGateway drives the full loop: the chat model
// requests the TextToVideo tool, the tool creates the job on /v1/videos and
// polls it, and the rendered MP4 is written into text_to_video.output_dir.
// The pinned tokenless does not serve the Videos API yet, so the three video
// routes run on a tiny in-test mock layered over the tokenless chat gateway,
// following the same job lifecycle (queued to completed after two polls).
func TestTextToVideoToolAgainstMockGateway(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: video-clip
    match: '(?i)render a video clip'
    turns:
      - tool_calls:
          - { name: TextToVideo, args: { prompt: "a neon city flyover", seconds: "4" } }
      - content: "Video generated."
`))
	require.NoError(t, err)
	std, err := mockgateway.LoadFile(filepath.Join(repoRoot(), "tests", "integration", "scenarios.yaml"))
	require.NoError(t, err)
	defs.Models = std.Models

	var mu sync.Mutex
	var createdForm map[string]string
	polls := 0

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/videos", func(w http.ResponseWriter, r *http.Request) {
		form := map[string]string{}
		_ = r.ParseMultipartForm(1 << 20)
		for _, key := range []string{"model", "prompt", "seconds", "size"} {
			if v := r.FormValue(key); v != "" {
				form[key] = v
			}
		}
		mu.Lock()
		createdForm = form
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"job-1","object":"video","created_at":1,"model":"creatify-aurora","status":"queued"}`)
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
		_, _ = fmt.Fprintf(w, `{"id":"job-1","object":"video","created_at":1,"model":"creatify-aurora","status":%q,"progress":100}`, status)
	})
	mux.HandleFunc("GET /v1/videos/{id}/content", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = fmt.Fprint(w, "fake-mp4-bytes")
	})
	mux.Handle("/", mockgateway.New(defs))

	videoDir := ""
	e := newEnvWithHandler(t, mux, func(cfg *config.Config) {
		cfg.TextToVideo.Enabled = true
		cfg.TextToVideo.OutputDir = filepath.Join(t.TempDir(), "video-out")
		cfg.TextToVideo.PollInterval = 1
		videoDir = cfg.TextToVideo.OutputDir
	})
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	req := &agentdomain.AgentRequest{
		RequestID: "req-text-to-video",
		Model:     testModel,
		Messages:  []sdk.Message{userMessage(t, "render a video clip")},
	}
	events, err := e.container.GetAgentService().RunWithStream(ctx, req)
	require.NoError(t, err)

	var (
		content     string
		videoResult *agentdomain.ToolExecutionResult
		completed   bool
		errs        []error
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
					if res.ToolName == "TextToVideo" {
						videoResult = res
					}
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for the agent event channel to close")
		}
	}

	require.Empty(t, errs)
	require.True(t, completed)
	require.Contains(t, content, "Video generated.")

	require.NotNil(t, videoResult, "expected a TextToVideo tool result")
	res := videoResult
	require.True(t, res.Success, res.Error)
	data, ok := res.Data.(map[string]any)
	require.True(t, ok, "tool result data must be a map, got %T", res.Data)
	require.Equal(t, "elevenlabs/creatify-aurora", data["model"])
	require.Equal(t, "4", data["seconds"])

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "creatify-aurora", createdForm["model"])
	require.Equal(t, "a neon city flyover", createdForm["prompt"])
	require.Equal(t, "4", createdForm["seconds"])
	require.Equal(t, 2, polls, "one poll for the queued state, one for completed")

	entries, err := filepath.Glob(filepath.Join(videoDir, "*.mp4"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one saved clip")
	require.Equal(t, entries[0], data["path"])
}
