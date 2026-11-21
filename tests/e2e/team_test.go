//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	tokenless "github.com/inference-gateway/tokenless"
)

// awaitRequest waits until the mock answered a request carrying text, as the
// agents of a team talk on their own after the lead's turn ends.
func awaitRequest(t *testing.T, m *tokenless.Mock, text string, d *daemonProc) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range m.Requests() {
			if strings.Contains(string(r.RawBody), text) {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no request carried %q\ndaemon output:\n%s", text, d.output)
}

// TestDaemonTeamMessagesReachTeammatesAndTheLead spawns a team of two from a
// daemon thread: the scout messages the keeper through the lead, and the
// keeper, answering what it heard, messages the lead.
func TestDaemonTeamMessagesReachTeammatesAndTheLead(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	m := startMock(t)
	d := startDaemon(t, home, "INFER_GATEWAY_MOCK=false", "INFER_GATEWAY_RUN=false", "INFER_GATEWAY_URL="+m.URL)
	c := dialDaemon(t, d, "desktop")
	c.send(map[string]any{"type": "new_session", "project_dir": project, "mode": "auto", "model": testModel})
	c.next("MESSAGES_SNAPSHOT")

	c.say("assemble a team")
	require.Contains(t, replyText(c.run()), "The team is working.")

	awaitRequest(t, m, `[Message from scout]\n\nteam-secret-42`, d)
	awaitRequest(t, m, `[Message from keeper]\n\nthe keeper heard the secret`, d)
	require.NotEmpty(t, requestsFor(m, "team-keeper-relay"), "the keeper must answer the scout's message")
}

// TestDaemonPlacesATeammateOnItsAgentsComputer spawns a Markdown agent whose
// workspace is another daemon's: its thread teleports there before the first
// turn, and its message still reaches the lead at home.
func TestDaemonPlacesATeammateOnItsAgentsComputer(t *testing.T) {
	homeA, homeB := t.TempDir(), t.TempDir()
	writeUserAgent(t, homeA, ".infer", "e2e-researcher", "workspace: b\n", "You research on your own computer.")
	m := startMock(t)
	a, b, projectsRoot := peersIn(t, homeA, homeB, "", "INFER_GATEWAY_MOCK=false", "INFER_GATEWAY_RUN=false", "INFER_GATEWAY_URL="+m.URL)
	project := gitProject(t)
	c := dialDaemon(t, a, "desktop")
	c.send(map[string]any{"type": "new_session", "project_dir": project, "mode": "auto", "model": testModel})
	c.next("MESSAGES_SNAPSHOT")

	c.say("assemble a remote team")
	require.Contains(t, replyText(c.run()), "The researcher is on its way.")

	awaitRequest(t, m, `[Message from e2e-researcher]\n\nthe researcher reports from its own computer`, a)
	sessions, err := os.ReadDir(projectsRoot)
	require.NoError(t, err, "daemon B output:\n%s", b.output)
	require.NotEmpty(t, sessions, "the researcher's session must run on daemon B")
}
