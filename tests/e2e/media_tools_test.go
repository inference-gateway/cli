//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	tokenless "github.com/inference-gateway/tokenless"
)

// runAgentWithEnv is runAgent with extra INFER_* overrides layered on the
// hermetic preset.
func runAgentWithEnv(t *testing.T, gatewayURL, dir, prompt string, extra map[string]string) (string, int) {
	t.Helper()
	env := inferEnv(gatewayURL)
	for k, v := range extra {
		env[k] = v
	}
	res := tokenless.Orchestrator{Bin: binPath, Dir: dir, Env: env}.Run(t, "headless", prompt)
	return res.Stdout, res.ExitCode
}

// TestAgentTextToSFXWritesClip runs the built binary headless with the
// TextToSFX tool enabled; the mock gateway serves /v1/audio/sfx with a canned
// WAV, which must land in the configured output dir and be reported back to
// the model as a successful tool result.
func TestAgentTextToSFXWritesClip(t *testing.T) {
	m := startMock(t)
	outDir := filepath.Join(t.TempDir(), "sfx-out")

	stdout, code := runAgentWithEnv(t, m.URL, t.TempDir(), "generate a whoosh sound effect", map[string]string{
		"INFER_TEXT_TO_SFX_ENABLED":    "true",
		"INFER_TEXT_TO_SFX_OUTPUT_DIR": outDir,
	})
	require.Zero(t, code)

	lines := jsonLines(t, stdout)
	toolResults := contentsByRole(lines, "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], "a fast whoosh")
	require.NotContains(t, toolResults[0], "failed")
	require.Contains(t, contentsByRole(lines, "assistant"), "Sound effect generated.")

	clips, err := filepath.Glob(filepath.Join(outDir, "sfx-*.wav"))
	require.NoError(t, err)
	require.Len(t, clips, 1, "expected exactly one saved WAV")
	info, err := os.Stat(clips[0])
	require.NoError(t, err)
	require.Positive(t, info.Size())
}

// TestAgentTextToMusicWritesClip is the TextToMusic counterpart: the mock's
// /v1/audio/music answers with a canned MP3 that must be saved to disk.
func TestAgentTextToMusicWritesClip(t *testing.T) {
	m := startMock(t)
	outDir := filepath.Join(t.TempDir(), "music-out")

	stdout, code := runAgentWithEnv(t, m.URL, t.TempDir(), "compose a calm piano loop", map[string]string{
		"INFER_TEXT_TO_MUSIC_ENABLED":    "true",
		"INFER_TEXT_TO_MUSIC_OUTPUT_DIR": outDir,
	})
	require.Zero(t, code)

	lines := jsonLines(t, stdout)
	toolResults := contentsByRole(lines, "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], "calm piano loop")
	require.NotContains(t, toolResults[0], "failed")
	require.Contains(t, contentsByRole(lines, "assistant"), "Music clip composed.")

	clips, err := filepath.Glob(filepath.Join(outDir, "music-*.mp3"))
	require.NoError(t, err)
	require.Len(t, clips, 1, "expected exactly one saved MP3")
	info, err := os.Stat(clips[0])
	require.NoError(t, err)
	require.Positive(t, info.Size())
}

// TestChatTUITextToSFX drives the chat TUI inside tmux: the SFX tool runs
// against the mock gateway's /v1/audio/sfx and the model's confirmation must
// render in the pane, with the WAV saved under the configured output dir.
func TestChatTUITextToSFX(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; skipping TUI drive test")
	}

	const session = "infer-e2e-sfx"
	home := startTmuxHome(t, session)
	outDir := filepath.Join(home, "sfx-out")

	launch := "env HOME=" + home +
		" INFER_GATEWAY_MOCK=true INFER_STORAGE_ENABLED=false" +
		" INFER_GATEWAY_MOCK_SCENARIOS=" + filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml") +
		" INFER_TEXT_TO_SFX_ENABLED=true INFER_TEXT_TO_SFX_OUTPUT_DIR=" + outDir +
		" " + binPath + " chat"
	require.NoError(t, exec.Command("tmux", "new-session", "-d", "-s", session,
		"-x", "200", "-y", "50", launch).Run(), "failed to start tmux session")

	require.True(t, waitForPane(t, session, "Select a Model", 25*time.Second),
		"model picker never rendered; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Type your message", 20*time.Second),
		"input view never appeared after model select; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "-l", "generate a whoosh sound effect")
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Sound effect generated.", 30*time.Second),
		"the model never confirmed the sound effect; last frame:\n%s", capturePane(session))

	clips, err := filepath.Glob(filepath.Join(outDir, "sfx-*.wav"))
	require.NoError(t, err)
	require.Len(t, clips, 1, "expected exactly one saved WAV; last frame:\n%s", capturePane(session))
}
