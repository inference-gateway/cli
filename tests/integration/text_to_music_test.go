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

// TestTextToMusicToolAgainstMockGateway runs the TextToMusic tool against the
// mock gateway. The mock gateway (tokenless) does NOT serve POST
// /v1/audio/music, so this pins the current real behavior: the tool call
// fails with a one-line error naming the configured model, the agent run
// still completes, and no partial WAV file is left behind.
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
      - content: "Music clip attempted."
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
	require.True(t, completed, "agent run should complete despite the tool failure")
	require.Contains(t, content, "Music clip attempted.")

	require.Len(t, musicResults, 1, "expected exactly one TextToMusic tool result")
	res := musicResults[0]
	require.False(t, res.Success, "the mock gateway does not serve /v1/audio/music")
	require.Contains(t, res.Error, "music generation with elevenlabs/music_v2_5 failed")

	entries, err := os.ReadDir(musicDir)
	require.NoError(t, err)
	require.Empty(t, entries, "a failed music call must leave no partial file")
}
