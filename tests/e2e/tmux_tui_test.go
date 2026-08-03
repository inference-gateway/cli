//go:build e2e

package e2e

import (
	"os/exec"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	e2etest "github.com/inference-gateway/tokenless/e2etest"
)

// TestChatTUIViaTmux drives the built binary's chat TUI end-to-end inside a tmux
// session, following the procedure documented in the built-in `tmux` skill
// (internal/services/skills/builtins/tmux/SKILL.md) and the AGENTS.md recipe:
// start a session, select the single mock model, type a prompt, and read the
// reply back from the pane. Skipped where tmux is not installed.
func TestChatTUIViaTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; skipping TUI drive test")
	}

	const session = "infer-e2e-tui"
	_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() })

	launch := "env HOME=" + t.TempDir() +
		" INFER_GATEWAY_MOCK=true INFER_STORAGE_ENABLED=false " + binPath + " chat"
	require.NoError(t, exec.Command("tmux", "new-session", "-d", "-s", session,
		"-x", "200", "-y", "50", launch).Run(), "failed to start tmux session")

	require.True(t, waitForPane(t, session, "Select a Model", 25*time.Second),
		"model picker never rendered; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Type your message", 20*time.Second),
		"input view never appeared after model select; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "-l", "say hello")
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Hello! How can I help?", 25*time.Second),
		"the mock reply never appeared in the TUI; last frame:\n%s", capturePane(session))
}

// TestChatTUIBackgroundShellOutput drives the TUI to trigger a background shell
// and then opens /tasks to verify the "Output" section displays the captured
// stdout. This tests the full pipeline: shell stdout → OutputRingBuffer →
// shellJob.Output() → Supervisor.Snapshot() → TaskInfo.Output →
// renderJobOutput() in the TUI.
func TestChatTUIBackgroundShellOutput(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; skipping TUI drive test")
	}

	const session = "infer-e2e-shell-output"
	_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() })

	launch := "env HOME=" + t.TempDir() +
		" INFER_GATEWAY_MOCK=true INFER_STORAGE_ENABLED=false " + binPath + " chat"
	require.NoError(t, exec.Command("tmux", "new-session", "-d", "-s", session,
		"-x", "200", "-y", "50", launch).Run(), "failed to start tmux session")

	require.True(t, waitForPane(t, session, "Select a Model", 25*time.Second),
		"model picker never rendered; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Type your message", 20*time.Second),
		"input view never appeared after model select; last frame:\n%s", capturePane(session))

	tmuxSendKeys(t, session, "-l", "run a background shell")
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "The background shell task ran.", 30*time.Second),
		"agent never acknowledged the background shell; last frame:\n%s", capturePane(session))

	tmuxSendKeys(t, session, "-l", "/tasks")
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Output", 15*time.Second),
		"the /tasks panel never showed the Output section; last frame:\n%s", capturePane(session))

	require.True(t, waitForPane(t, session, "hello-from-background-shell", 10*time.Second),
		"the background shell output never appeared in /tasks; last frame:\n%s", capturePane(session))
}

// TestChatTUIBackgroundSubagentOutput launches a headless background subagent
// and verifies its final result appears in the /tasks "Output" detail section.
func TestChatTUIBackgroundSubagentOutput(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; skipping TUI drive test")
	}

	const session = "infer-e2e-subagent-output"
	_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() })

	launch := "env HOME=" + t.TempDir() +
		" INFER_GATEWAY_MOCK=true INFER_STORAGE_ENABLED=false" +
		" INFER_TOOLS_AGENT_MODE=headless INFER_TOOLS_AGENT_WAIT=false" +
		" INFER_TOOLS_AGENT_REQUIRE_APPROVAL=false " + binPath + " chat"
	require.NoError(t, exec.Command("tmux", "new-session", "-d", "-s", session,
		"-x", "200", "-y", "50", launch).Run(), "failed to start tmux session")

	require.True(t, waitForPane(t, session, "Select a Model", 25*time.Second),
		"model picker never rendered; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Type your message", 20*time.Second),
		"input view never appeared after model select; last frame:\n%s", capturePane(session))

	tmuxSendKeys(t, session, "-l", "launch a subagent")
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "The subagent task was launched.", 30*time.Second),
		"agent never acknowledged the subagent launch; last frame:\n%s", capturePane(session))

	tmuxSendKeys(t, session, "-l", "/tasks")
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Completed", 45*time.Second),
		"the subagent never completed in /tasks; last frame:\n%s", capturePane(session))

	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "hello-from-subagent-probe", 15*time.Second),
		"the subagent output never appeared in the detail panel; last frame:\n%s", capturePane(session))
}

func capturePane(session string) string {
	return e2etest.CapturePane(session)
}

func tmuxSendKeys(t *testing.T, session string, args ...string) {
	t.Helper()
	e2etest.SendKeys(t, session, args...)
}

func waitForPane(t *testing.T, session, want string, timeout time.Duration) bool {
	t.Helper()
	return e2etest.WaitForPane(t, session, want, timeout)
}
