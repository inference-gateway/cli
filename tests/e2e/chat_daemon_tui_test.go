//go:build e2e

package e2e

import (
	"fmt"
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
// conversation on environment, and closes the table again.
func expectListedOn(t *testing.T, session, environment string, daemons ...*daemonProc) {
	t.Helper()
	chatSay(t, session, "/conversations")
	expectPane(t, session, "Environment", daemons...)
	require.Regexp(t, `say hello\s+`+environment+`\s`, capturePane(session))
	tmuxSendKeys(t, session, "Escape")
	expectPane(t, session, "Type your message", daemons...)
}

func TestChatDaemonRunsATurnAndReattaches(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_ENVIRONMENT=a")
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

func TestChatDaemonMovesTheSessionAndBringsItBack(t *testing.T) {
	a, b, _ := peers(t)
	project := gitProject(t)
	const session = "infer-e2e-daemon-transfer"

	startDaemonChat(t, session, a, project)
	expectPane(t, session, "⌂ a", a)
	chatSay(t, session, "say hello")
	expectPane(t, session, "Hello! How can I help?", a)

	chatSay(t, session, "/transfer b")
	expectPane(t, session, "Session now runs on b", a, b)
	expectPane(t, session, "⇄ b", a, b)
	chatSay(t, session, "say hello again")
	expectPane(t, session, "You: say hello again", a, b)
	expectPane(t, session, "Response complete", a, b)
	expectListedOn(t, session, "b", a, b)
	chatSay(t, session, "/diff")
	expectPane(t, session, "/diff is unavailable: the session runs on b", a, b)
	chatSay(t, session, "say hello @README.md")
	expectPane(t, session, "You: say hello @README.md", a, b)
	require.NotContains(t, capturePane(session), "transfer me", "a remote session read this machine's file:\n%s", capturePane(session))

	chatSay(t, session, "/transfer a")
	expectPane(t, session, "Session now runs on a", a, b)
	expectPane(t, session, "⌂ a", a, b)
	require.NotContains(t, capturePane(session), "⇄ b", "the indicator still names b:\n%s", capturePane(session))
	expectListedOn(t, session, "a", a, b)
}
