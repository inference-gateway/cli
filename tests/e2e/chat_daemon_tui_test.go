//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"
)

// startDaemonChat runs infer chat --daemon in project inside a tmux session,
// dialing daemon d. The session outlives the chat, so its exit line stays on
// screen. Start the daemons first: their teardown then runs after the
// session's, so the chat never sees them go and starts one of its own.
func startDaemonChat(t *testing.T, session string, d *daemonProc, project string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	home := startTmuxHome(t, session)
	launch := fmt.Sprintf("cd %s && env HOME=%s INFER_GATEWAY_MOCK=true INFER_GATEWAY_MOCK_SCENARIOS=%s INFER_AGENT_MODEL=%s "+
		"INFER_DAEMON_BINDING_PORT=%d INFER_DAEMON_BINDING_TOKEN=%s %s chat --daemon %s; sleep 120",
		project, home, filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml"), testModel,
		d.port, daemonToken, binPath, strings.Join(args, " "))
	require.NoError(t, exec.Command("tmux", "new-session", "-d", "-s", session, "-x", "200", "-y", "50", launch).Run())
}

// expectPane waits for want on the session's screen and fails with the screen
// and the daemons' output when it never shows.
func expectPane(t *testing.T, session, want string, daemons ...*daemonProc) {
	t.Helper()
	if waitForPane(t, session, want, 60*time.Second) {
		return
	}
	output := ""
	for _, d := range daemons {
		output += d.output.String()
	}
	t.Fatalf("%q never showed, last frame:\n%s\ndaemon output:\n%s", want, capturePane(session), output)
}

func chatSay(t *testing.T, session, text string) {
	t.Helper()
	tmuxSendKeys(t, session, "-l", text)
	tmuxSendKeys(t, session, "Enter")
}

// expectListedOn opens the conversations table, expects the session's
// conversation on workspace, and closes the table again.
func expectListedOn(t *testing.T, session, workspace string, daemons ...*daemonProc) {
	t.Helper()
	chatSay(t, session, "/conversations")
	expectPane(t, session, "Workspace", daemons...)
	require.Regexp(t, `say hello\s+`+workspace+`\s`, capturePane(session))
	tmuxSendKeys(t, session, "Escape")
	expectPane(t, session, "Type your message", daemons...)
}

func TestChatDaemonRunsATurnAndReattaches(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a")
	project := gitProject(t)

	startDaemonChat(t, "infer-e2e-daemon-chat", d, project)
	expectPane(t, "infer-e2e-daemon-chat", "⌂ a", d)
	chatSay(t, "infer-e2e-daemon-chat", "say hello")
	expectPane(t, "infer-e2e-daemon-chat", "Hello! How can I help?", d)
	chatSay(t, "infer-e2e-daemon-chat", "/exit")
	expectPane(t, "infer-e2e-daemon-chat", "Reattach with", d)

	id := regexp.MustCompile(`--session-id ([0-9a-f-]{36})`).FindStringSubmatch(capturePane("infer-e2e-daemon-chat"))
	require.Len(t, id, 2, "the exit line names no session:\n%s", capturePane("infer-e2e-daemon-chat"))
	startDaemonChat(t, "infer-e2e-daemon-reattach", d, project, "--session-id", id[1])
	expectPane(t, "infer-e2e-daemon-reattach", "Hello! How can I help?", d)
	expectPane(t, "infer-e2e-daemon-reattach", "⌂ a", d)
}

// TestChatDaemonReportsUsageAsEachStepEnds reads the provider's usage off the
// status bar while the run is still on its next step, as a local chat shows it.
func TestChatDaemonReportsUsageAsEachStepEnds(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a")
	project := gitProject(t)

	const session = "infer-e2e-daemon-usage"
	startDaemonChat(t, session, d, project)
	expectPane(t, session, "⌂ a", d)
	chatSay(t, session, "usage per step probe")
	expectPane(t, session, "T.1k • C.400 • $0.0021", d)
	require.NotContains(t, capturePane(session), "USAGE-PROBE-DONE", "the reply must still be on its way when the usage shows")
	expectPane(t, session, "$0.0021 • 3s", d)
	expectPane(t, session, "USAGE-PROBE-DONE", d)
	expectPane(t, session, "T.2.5k • C.1.3k", d)
}

// TestChatDaemonFollowsABackgroundJob follows a job a daemon chat's worker runs:
// its note lands in the chat and the model answers it unprompted.
func TestChatDaemonFollowsABackgroundJob(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a")
	project := gitProject(t)

	const session = "infer-e2e-daemon-job"
	startDaemonChat(t, session, d, project)
	expectPane(t, session, "⌂ a", d)
	chatSay(t, session, "background idle probe")
	expectPane(t, session, "IDLE-PROBE-ACK", d)
	expectPane(t, session, "Background Shell Completed", d)
	expectPane(t, session, "IDLE-PROBE-FOLLOW-UP", d)
}

// TestChatDaemonListsAndStopsTheWorkersJobs drives /tasks over a job that runs in
// the worker, not in the chat: it is listed as running, stops when cancelled and
// stays listed as ended.
func TestChatDaemonListsAndStopsTheWorkersJobs(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a")
	project := gitProject(t)

	const session = "infer-e2e-daemon-tasks"
	startDaemonChat(t, session, d, project)
	expectPane(t, session, "⌂ a", d)
	chatSay(t, session, "run a long background shell")
	expectPane(t, session, "The long shell is running.", d)

	chatSay(t, session, "/tasks")
	expectPane(t, session, "│ Running", d)
	tmuxSendKeys(t, session, "c", "y")
	expectPane(t, session, "│ Failed", d)
}

func TestChatDaemonKeepsARunsStatusAfterClosingTasks(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a")
	project := gitProject(t)

	const session = "infer-e2e-daemon-panel-status"
	startDaemonChat(t, session, d, project)
	expectPane(t, session, "⌂ a", d)

	expectStatusSurvivesPanel(t, session, "/tasks", "Background Tasks", d)
}

// TestChatDaemonShowsAFinishedJobsOutput pins the output of a job that ran in the
// worker, which only the worker holds.
func TestChatDaemonShowsAFinishedJobsOutput(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a")
	project := gitProject(t)

	const session = "infer-e2e-daemon-task-output"
	startDaemonChat(t, session, d, project)
	expectPane(t, session, "⌂ a", d)
	chatSay(t, session, "run a background shell")
	expectPane(t, session, "The background shell task ran.", d)

	chatSay(t, session, "/tasks")
	expectPane(t, session, completedTaskRow, d)
	tmuxSendKeys(t, session, "i")
	expectPane(t, session, "Output", d)
	expectPane(t, session, "hello-from-background-shell", d)
}

// TestChatDaemonShowsASubagentsTranscript pins the detail of a subagent a worker
// ran, whose conversation the worker's storage holds.
func TestChatDaemonShowsASubagentsTranscript(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a", "INFER_TOOLS_AGENT_MODE=headless",
		"INFER_TOOLS_AGENT_WAIT=false", "INFER_TOOLS_AGENT_REQUIRE_APPROVAL=false")
	project := gitProject(t)

	const session = "infer-e2e-daemon-subagent"
	startDaemonChat(t, session, d, project)
	expectPane(t, session, "⌂ a", d)
	chatSay(t, session, "launch a subagent")
	expectPane(t, session, "The subagent task was launched.", d)
	expectPane(t, session, "Subagent Completed", d)

	chatSay(t, session, "/tasks")
	expectPane(t, session, completedTaskRow, d)
	tmuxSendKeys(t, session, "i")
	expectPane(t, session, "Transcript", d)
	expectPane(t, session, "hello-from-subagent-probe", d)
}

func TestChatDaemonRunsDirectCommandsLikeAPlainChat(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a")
	project := gitProject(t)
	shortcuts := filepath.Join(project, ".infer", "shortcuts")
	require.NoError(t, os.MkdirAll(shortcuts, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(shortcuts, "say.yaml"),
		[]byte("shortcuts:\n  - name: say\n    description: says something\n    command: echo\n    args:\n      - kept-note\n"), 0o644))

	const session = "infer-e2e-daemon-direct"
	startDaemonChat(t, session, d, project)
	expectPane(t, session, "⌂ a", d)

	chatSay(t, session, "!for i in 1 2 3 4; do echo streamed-$i; sleep 1; done")
	expectPane(t, session, "streamed-1", d)
	expectPane(t, session, "Executing:", d)
	expectPane(t, session, "✓ Bash(", d)
	require.Contains(t, capturePane(session), "! for i in", "the typed command left the conversation")

	chatSay(t, session, "/say")
	expectPane(t, session, "kept-note", d)
	chatSay(t, session, "say hello")
	expectPane(t, session, "Hello! How can I help?", d)
	require.Contains(t, capturePane(session), "kept-note", "the next turn dropped the shortcut's output")

	chatSay(t, session, `!!Nope(a="b")`)
	expectPane(t, session, "tool Nope is not enabled", d)
	chatSay(t, session, "anything else")
	expectPane(t, session, "Done.", d)

	chatSay(t, session, "/new")
	expectPane(t, session, "Starting new conversation", d)
	chatSay(t, session, "/say")
	expectPane(t, session, "kept-note", d)
	chatSay(t, session, "say hello")
	expectPane(t, session, "Hello! How can I help?", d)
	require.Contains(t, capturePane(session), "kept-note", "the first turn of a new conversation dropped the shortcut's output")
}

func TestChatDaemonTeleportsTheSessionAndBringsItBack(t *testing.T) {
	a, b, _ := peers(t)
	project := gitProject(t)
	const session = "infer-e2e-daemon-teleport"

	startDaemonChat(t, session, a, project)
	expectPane(t, session, "⌂ a", a)
	chatSay(t, session, "say hello")
	expectPane(t, session, "Hello! How can I help?", a)

	chatSay(t, session, "/teleport b")
	expectPane(t, session, "Session now runs on b", a, b)
	expectPane(t, session, "⇄ b", a, b)
	chatSay(t, session, "say hello again")
	expectPane(t, session, "You: say hello again", a, b)
	expectPane(t, session, "Response complete", a, b)
	expectListedOn(t, session, "b", a, b)
	chatSay(t, session, "/diff")
	expectPane(t, session, "/diff is unavailable: the session runs on b", a, b)
	localOnly := filepath.Join(project, "local.md")
	require.NoError(t, os.WriteFile(localOnly, []byte("only on a\n"), 0644))
	chatSay(t, session, "say hello @local.md")
	expectPane(t, session, "You: say hello @local.md", a, b)
	require.NotContains(t, capturePane(session), "only on a", "a remote session read this machine's file:\n%s", capturePane(session))
	require.NoError(t, os.Remove(localOnly))

	chatSay(t, session, "/teleport a")
	expectPane(t, session, "Session now runs on a", a, b)
	expectPane(t, session, "⌂ a", a, b)
	require.NotContains(t, capturePane(session), "⇄ b", "the indicator still names b:\n%s", capturePane(session))
	expectListedOn(t, session, "a", a, b)
}
