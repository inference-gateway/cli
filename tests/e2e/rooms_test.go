//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

// pinnedAgent is a Markdown agent whose file names workspace b as its computer.
const pinnedAgent = "editor"

// roomPostBy waits for the next room post the member wrote.
func roomPostBy(c *daemonClient, author string) map[string]any {
	c.t.Helper()
	for {
		frame := c.next("room_post")
		post, _ := frame["post"].(map[string]any)
		if post["author"] == author {
			return post
		}
	}
}

// TestARoomMemberPinnedToAWorkspaceAnswersFromThere posts to a room on daemon A
// whose member's agent pins workspace b: the member's new session moves to
// daemon B before its first turn, answers in the room, and lives on B.
func TestARoomMemberPinnedToAWorkspaceAnswersFromThere(t *testing.T) {
	homeA, homeB := t.TempDir(), t.TempDir()
	project := gitProject(t)
	agents := filepath.Join(homeA, ".infer", "agents")
	require.NoError(t, os.MkdirAll(agents, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(agents, pinnedAgent+".md"),
		[]byte("---\nname: "+pinnedAgent+"\ndescription: Edits footage\nworkspace: b\n---\n\nYou edit footage.\n"), 0644))
	rooms := fmt.Sprintf("router:\n  model: \"\"\nrooms:\n  - name: release\n    project: %s\n    mode: auto\n    agents:\n      - %s\n", project, pinnedAgent)
	require.NoError(t, os.WriteFile(filepath.Join(homeA, ".infer", "rooms.yaml"), []byte(rooms), 0644))
	a, b, projectsRoot := peersIn(t, homeA, homeB, "")

	user := dialDaemon(t, a, "desktop")
	user.send(map[string]any{"type": "follow_room", "room": "release"})
	user.next("room_snapshot")
	user.send(map[string]any{"type": "post_room", "room": "release", "text": "@" + pinnedAgent + " say hello"})
	posted := user.next("room_posted")
	require.NotEmpty(t, posted["post"].(map[string]any)["routes"], "the ping routes to the member")

	answer := roomPostBy(user, pinnedAgent)
	require.NotEmpty(t, answer["text"], "daemon A:\n%s\ndaemon B:\n%s", a.output, b.output)

	session := "room-release-" + pinnedAgent
	placed, err := filepath.Glob(filepath.Join(projectsRoot, "*", session))
	require.NoError(t, err)
	require.Len(t, placed, 1, "the member's session got a workspace dir on daemon B")
	direct := dialDaemon(t, b, "desktop")
	direct.send(map[string]any{"type": "resume_conversation", "project_dir": placed[0], "id": session})
	require.True(t, snapshotContains(direct.next("MESSAGES_SNAPSHOT"), answer["text"].(string)), "daemon B holds the member's session and its answer")
}
