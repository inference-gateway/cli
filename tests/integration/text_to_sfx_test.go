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

// TestTextToSFXToolAgainstMockGateway drives the full loop: the chat model
// requests the TextToSFX tool, the tool posts the configured model and prompt
// to /v1/audio/sfx (served by tokenless with a canned WAV), and the clip is
// written into text_to_sfx.output_dir with a readable duration.
func TestTextToSFXToolAgainstMockGateway(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: sfx-clip
    match: '(?i)generate a whoosh sound effect'
    turns:
      - tool_calls:
          - { name: TextToSFX, args: { prompt: "a fast whoosh", seconds: 2 } }
      - content: "Sound effect generated."
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
	require.True(t, completed)
	require.Contains(t, content, "Sound effect generated.")

	require.Len(t, sfxResults, 1, "expected exactly one TextToSFX tool result")
	res := sfxResults[0]
	require.True(t, res.Success, res.Error)
	data, ok := res.Data.(map[string]any)
	require.True(t, ok, "tool result data must be a map, got %T", res.Data)
	require.Equal(t, "a fast whoosh", data["prompt"])
	require.Greater(t, data["duration_seconds"], 0.0, "the canned WAV must have a readable duration")

	var sfxReqs []mockgateway.Recorded
	for _, rec := range e.gateway.Requests() {
		if rec.SFXBody != nil {
			sfxReqs = append(sfxReqs, rec)
		}
	}
	require.Len(t, sfxReqs, 1)
	rec := sfxReqs[0]
	require.Equal(t, "/v1/audio/sfx", rec.Endpoint)
	require.Equal(t, "elevenlabs", rec.Provider)
	require.Equal(t, "eleven_text_to_sound_v2", rec.Model)
	require.Equal(t, "a fast whoosh", rec.SFXBody.Prompt)
	require.NotNil(t, rec.SFXBody.DurationSeconds)
	require.InDelta(t, 2, *rec.SFXBody.DurationSeconds, 0.001)
	require.NotNil(t, rec.SFXBody.ResponseFormat)
	require.Equal(t, "wav", *rec.SFXBody.ResponseFormat)

	entries, err := os.ReadDir(sfxDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one saved clip")
	require.Equal(t, filepath.Join(sfxDir, entries[0].Name()), data["path"])
	require.Equal(t, ".wav", filepath.Ext(entries[0].Name()))
}
