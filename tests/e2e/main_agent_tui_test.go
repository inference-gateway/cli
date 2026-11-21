//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

// focusedAgentIndicator is how the agent indicator reads once the status row
// holds focus: a selected indicator is padded. Down focuses the row through an
// event, so an Enter sent before it lands would reach the input instead.
const focusedAgentIndicator = "Agent: main  •"

// switchToExplore focuses the status row, opens the agent dropdown from its
// first indicator and picks explore, listed right below main.
func switchToExplore(t *testing.T, session string, daemons ...*daemonProc) {
	t.Helper()
	tmuxSendKeys(t, session, "Down")
	expectPane(t, session, focusedAgentIndicator, daemons...)
	tmuxSendKeys(t, session, "Enter")
	expectPane(t, session, "Switch agent", daemons...)
	tmuxSendKeys(t, session, "Down")
	tmuxSendKeys(t, session, "Enter")
	expectPane(t, session, "Agent: explore", daemons...)
}

// expectExploreRefusesTheWrite sends a prompt the model answers with a Write,
// which explore may not call: the call is refused without asking for an
// approval, nothing is written and the model gets its next turn.
func expectExploreRefusesTheWrite(t *testing.T, session, dir string, daemons ...*daemonProc) {
	t.Helper()
	chatSay(t, session, "main agent write probe")
	expectPane(t, session, `Write is not available to agent "explore"`, daemons...)
	expectPane(t, session, "MAIN-AGENT-PROBE-DONE", daemons...)
	require.NotContains(t, capturePane(session), "Approval required")
	require.NoFileExists(t, filepath.Join(dir, "main-agent-probe.txt"))
}

func TestChatSwitchesItsMainAgent(t *testing.T) {
	const session = "infer-e2e-main-agent"
	dir := startLocalChat(t, session)
	chatSay(t, session, "say hello")
	expectPane(t, session, "Hello! How can I help?")

	switchToExplore(t, session)
	expectExploreRefusesTheWrite(t, session, dir)
	expectPane(t, session, "Hello! How can I help?")
}

func TestChatDaemonSwitchesItsMainAgent(t *testing.T) {
	d := startDaemon(t, t.TempDir(), "INFER_DAEMON_WORKSPACE=a")
	project := gitProject(t)

	const session = "infer-e2e-daemon-main-agent"
	startDaemonChat(t, session, d, project)
	expectPane(t, session, "⌂ a", d)
	chatSay(t, session, "say hello")
	expectPane(t, session, "Hello! How can I help?", d)

	switchToExplore(t, session, d)
	expectExploreRefusesTheWrite(t, session, project, d)
	expectPane(t, session, "Hello! How can I help?", d)
}
