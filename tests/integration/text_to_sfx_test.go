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

// TestTextToSFXToolAgainstMockGateway runs the TextToSFX tool against the
// mock gateway. The mock gateway (tokenless) does NOT serve POST
// /v1/audio/sfx, so this pins the current real behavior: the tool call
// fails with a one-line error naming the configured model, the agent run
// still completes, and no partial file is left behind.
func TestTextToSFXToolAgainstMockGateway(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: sfx-clip
    match: '(?i)generate a whoosh sound effect'
    turns:
      - tool_calls:
          - { name: TextToSFX, args: { prompt: "a fast whoosh" } }
      - content: "Sound effect attempted."
`))
	require.NoError(t, err)

	sfxDir := ""
	e := newEnvWithScenarios(t, defs, func(cfg *config.Config) {
		cfg.TextToSFX.Enabled = true
		cfg.TextToSFX.OutputDir = filepath.Join(t.TempDir(), "sfx-out")
		sfxDir = cfg.TextToSFX.OutputDir
	})
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	req := &agentdomain.AgentRequest{
		RequestID: "req-text-to-sfx",
		Model:     testModel,
		Messages:  []sdk.Message{userMessage(t, "generate a whoosh sound effect")},
	}
	events, err := e.container.GetAgentService().RunWithStream(ctx, req)
	require.NoError(t, err)

	var (
		content    string
		sfxResults []*agentdomain.ToolExecutionResult
		completed  bool
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
					if res.ToolName == "TextToSFX" {
						sfxResults = append(sfxResults, res)
					}
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for the agent event channel to close")
		}
	}

	require.Empty(t, errs)
	require.True(t, completed, "agent run should complete despite the tool failure")
	require.Contains(t, content, "Sound effect attempted.")

	require.Len(t, sfxResults, 1, "expected exactly one TextToSFX tool result")
	res := sfxResults[0]
	require.False(t, res.Success, "the mock gateway does not serve /v1/audio/sfx")
	require.Contains(t, res.Error, "sfx generation with elevenlabs/eleven_text_to_sound_v2 failed")

	entries, err := os.ReadDir(sfxDir)
	require.NoError(t, err)
	require.Empty(t, entries, "a failed sfx call must leave no partial file")
}
