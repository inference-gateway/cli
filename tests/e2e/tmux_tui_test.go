//go:build e2e

package e2e

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	tokenless "github.com/inference-gateway/tokenless"

	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// TestChatTUIViaTmux drives the built binary's chat TUI end-to-end inside a tmux
// session, following the procedure documented in the built-in `tmux` skill
// (internal/skills/builtins/tmux/SKILL.md) and the AGENTS.md recipe:
// start a session, select the single mock model, type a prompt, and read the
// reply back from the pane. Skipped where tmux is not installed.
func TestChatTUIViaTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; skipping TUI drive test")
	}

	const session = "infer-e2e-tui"
	home := startTmuxHome(t, session)

	launch := "env HOME=" + home +
		" INFER_GATEWAY_MOCK=true INFER_STORAGE_ENABLED=false" +
		" INFER_GATEWAY_MOCK_SCENARIOS=" + filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml") +
		" " + binPath + " chat"
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
	home := startTmuxHome(t, session)

	launch := "env HOME=" + home +
		" INFER_GATEWAY_MOCK=true INFER_STORAGE_ENABLED=false" +
		" INFER_GATEWAY_MOCK_SCENARIOS=" + filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml") +
		" " + binPath + " chat"
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

// TestChatTUIBackgroundSubagentOutput verifies that, with storage disabled, a
// finished headless subagent's final result appears in the /tasks "Output"
// detail section.
func TestChatTUIBackgroundSubagentOutput(t *testing.T) {
	openFinishedSubagentDetail(t, "infer-e2e-subagent-output", "false",
		"Output", "hello-from-subagent-probe")
}

// TestChatTUIBackgroundSubagentTranscript verifies that a finished headless
// subagent's stored conversation and run stats appear in the /tasks detail panel.
func TestChatTUIBackgroundSubagentTranscript(t *testing.T) {
	openFinishedSubagentDetail(t, "infer-e2e-subagent-transcript", "true",
		"Transcript", "subagent-e2e-probe task", "hello-from-subagent-probe", "Tokens:")
}

// completedTaskRow is the status cell of a finished row in the loaded /tasks
// list. A bare "Completed" also matches the completion note in the chat, which
// is on screen before /tasks opens.
const completedTaskRow = "│ Completed"

// openFinishedSubagentDetail launches a headless background subagent, opens its
// /tasks detail panel once it completed and waits for every wanted string.
func openFinishedSubagentDetail(t *testing.T, session, storageEnabled string, want ...string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; skipping TUI drive test")
	}

	home := startTmuxHome(t, session)

	launch := "env HOME=" + home +
		" INFER_GATEWAY_MOCK=true INFER_STORAGE_ENABLED=" + storageEnabled +
		" INFER_GATEWAY_MOCK_SCENARIOS=" + filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml") +
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

	require.True(t, waitForPane(t, session, completedTaskRow, 45*time.Second),
		"the subagent never completed in /tasks; last frame:\n%s", capturePane(session))

	tmuxSendKeys(t, session, "Enter")

	for _, text := range want {
		require.True(t, waitForPane(t, session, text, 15*time.Second),
			"%q never appeared in the detail panel; last frame:\n%s", text, capturePane(session))
	}
}

// TestChatTUIApprovalBoxFollowsTail pins the follow-tail contract: opening the
// approval box shrinks the conversation viewport, which used to drop follow
// mode and leave the final reply below the fold after approval.
func TestChatTUIApprovalBoxFollowsTail(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; skipping TUI drive test")
	}

	const session = "infer-e2e-approval-tail"
	home := startTmuxHome(t, session)

	launch := "env HOME=" + home +
		" INFER_GATEWAY_MOCK=true INFER_STORAGE_ENABLED=false" +
		" INFER_GATEWAY_MOCK_SCENARIOS=" + filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml") +
		" " + binPath + " chat"
	require.NoError(t, exec.Command("tmux", "new-session", "-d", "-s", session,
		"-x", "200", "-y", "50", "-c", t.TempDir(), launch).Run(), "failed to start tmux session")

	require.True(t, waitForPane(t, session, "Select a Model", 25*time.Second),
		"model picker never rendered; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "Type your message", 20*time.Second),
		"input view never appeared after model select; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "-l", "fill the screen")
	tmuxSendKeys(t, session, "Enter")
	require.True(t, waitForPane(t, session, "filler 40", 25*time.Second),
		"filler reply never appeared; last frame:\n%s", capturePane(session))

	tmuxSendKeys(t, session, "-l", "approval tail probe")
	tmuxSendKeys(t, session, "Enter")
	require.True(t, waitForPane(t, session, "Approval required", 25*time.Second),
		"approval box never opened; last frame:\n%s", capturePane(session))
	require.Contains(t, capturePane(session), "approval tail probe",
		"the prompt must stay visible above the approval box")

	tmuxSendKeys(t, session, "Enter")
	require.True(t, waitForPane(t, session, "m2.txt", 25*time.Second),
		"second approval never opened; last frame:\n%s", capturePane(session))
	require.True(t, waitForPane(t, session, "Created m1.txt", 10*time.Second),
		"the first Write result stayed hidden behind the approval box; last frame:\n%s", capturePane(session))
	tmuxSendKeys(t, session, "Enter")

	require.True(t, waitForPane(t, session, "APPROVAL-TAIL-DONE", 25*time.Second),
		"the final reply stayed below the fold after approval; last frame:\n%s", capturePane(session))
}

// startTmuxHome returns a temp HOME for a chat process driven in the tmux
// session and registers its teardown. HOME is created before the kill-session
// cleanup so cleanups (LIFO) kill the process before TempDir removes the dir.
// kill-session only sends SIGHUP, and infer's shutdown handler still writes to
// HOME, so the teardown waits for the pane process to exit; removing HOME any
// earlier races it ("directory not empty").
func startTmuxHome(t *testing.T, session string) string {
	t.Helper()
	home := t.TempDir()
	_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	t.Cleanup(func() {
		out, _ := exec.Command("tmux", "display-message", "-p", "-t", session, "#{pane_pid}").Output()
		_ = exec.Command("tmux", "kill-session", "-t", session).Run()
		pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
		deadline := time.Now().Add(5 * time.Second)
		for err == nil && utils.ProcessAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	})
	return home
}

func capturePane(session string) string {
	return tokenless.CapturePane(session)
}

func tmuxSendKeys(t *testing.T, session string, args ...string) {
	t.Helper()
	tokenless.SendKeys(t, session, args...)
}

func waitForPane(t *testing.T, session, want string, timeout time.Duration) bool {
	t.Helper()
	return tokenless.WaitForPane(t, session, want, timeout)
}
