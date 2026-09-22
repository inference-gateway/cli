package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"
	mockgateway "github.com/inference-gateway/tokenless/gateway"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TestTextToMusicToolAgainstMockGateway drives the full loop: the chat model
// requests the TextToMusic tool, the tool posts the configured model, prompt
// and instrumental flag to /v1/audio/music (served by tokenless with a canned
// MP3), and the clip is written into text_to_music.output_dir.
func TestTextToMusicToolAgainstMockGateway(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: music-clip
    match: '(?i)compose a calm piano loop'
    turns:
      - tool_calls:
          - { name: TextToMusic, args: { prompt: "calm piano loop", instrumental: true } }
      - content: "Music clip composed."
`))
	require.NoError(t, err)

	musicDir := ""
	e := newEnvWithScenarios(t, defs, func(cfg *config.Config) {
		cfg.TextToMusic.Enabled = true
		cfg.TextToMusic.OutputDir = filepath.Join(t.TempDir(), "music-out")
		musicDir = cfg.TextToMusic.OutputDir
	})
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	req := &agentdomain.AgentRequest{
		RequestID: "req-text-to-music",
		Model:     testModel,
		Messages:  []sdk.Message{userMessage(t, "compose a calm piano loop")},
	}
	events, err := e.container.GetAgentService().RunWithStream(ctx, req)
	require.NoError(t, err)

	var (
		content      string
		musicResults []*agentdomain.ToolExecutionResult
		completed    bool
		errs         []error
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
					if res.ToolName == "TextToMusic" {
						musicResults = append(musicResults, res)
					}
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for the agent event channel to close")
		}
	}

	require.Empty(t, errs)
	require.True(t, completed)
	require.Contains(t, content, "Music clip composed.")

	require.Len(t, musicResults, 1, "expected exactly one TextToMusic tool result")
	res := musicResults[0]
	require.True(t, res.Success, res.Error)
	data, ok := res.Data.(map[string]any)
	require.True(t, ok, "tool result data must be a map, got %T", res.Data)
	require.Equal(t, "calm piano loop", data["prompt"])

	var musicReqs []mockgateway.Recorded
	for _, rec := range e.gateway.Requests() {
		if rec.MusicBody != nil {
			musicReqs = append(musicReqs, rec)
		}
	}
	require.Len(t, musicReqs, 1)
	rec := musicReqs[0]
	require.Equal(t, "/v1/audio/music", rec.Endpoint)
	require.Equal(t, "elevenlabs", rec.Provider)
	require.Equal(t, "music_v2_5", rec.Model)
	require.Equal(t, "calm piano loop", rec.MusicBody.Prompt)
	require.NotNil(t, rec.MusicBody.Instrumental)
	require.True(t, *rec.MusicBody.Instrumental)

	entries, err := os.ReadDir(musicDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one saved clip")
	require.Equal(t, filepath.Join(musicDir, entries[0].Name()), data["path"])
	info, err := entries[0].Info()
	require.NoError(t, err)
	require.Positive(t, info.Size(), "the saved clip must not be empty")
}
