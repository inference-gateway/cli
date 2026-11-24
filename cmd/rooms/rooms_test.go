package rooms_test

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	require "github.com/stretchr/testify/require"

	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"

	root "github.com/inference-gateway/cli/cmd/root"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
	projects "github.com/inference-gateway/cli/internal/projects"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
	roomsinfra "github.com/inference-gateway/cli/internal/rooms/infrastructure"
)

// testBindingToken authenticates the commands to the test daemon.
const testBindingToken = "test-token"

// isolate points HOME and the working directory at fresh temp dirs and blanks
// every INFER_* variable, so a run reads neither this machine's config nor the
// repository's .infer. It returns the working directory, the public room's
// project.
func isolate(t *testing.T) string {
	t.Helper()
	for _, env := range os.Environ() {
		if key, _, ok := strings.Cut(env, "="); ok && strings.HasPrefix(key, "INFER_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	t.Chdir(project)
	return project
}

func runInfer(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	command := root.NewCommand()
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(args)
	require.NoError(t, command.ExecuteContext(t.Context()), out.String())
	return out.String()
}

func TestShowRoomPrintsItsMembersAndLog(t *testing.T) {
	project := isolate(t)
	agents := filepath.Join(project, ".infer", "agents")
	require.NoError(t, os.MkdirAll(agents, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agents, "tester.md"), []byte("---\nname: tester\ndescription: Runs the tests.\n---\n\nRun the tests.\n"), 0o600))

	var view struct {
		Room struct {
			Members []struct {
				Agent     string `json:"agent"`
				SessionID string `json:"session_id"`
			} `json:"members"`
		} `json:"room"`
		Posts []roomsdomain.Post `json:"posts"`
	}
	out := runInfer(t, "rooms", "show", "public", "--format", "json")
	require.NoError(t, json.Unmarshal([]byte(out), &view), out)
	require.NotNil(t, view.Posts, "an unwritten log prints as [] for the desktop, not null")
	require.Equal(t, []struct {
		Agent     string `json:"agent"`
		SessionID string `json:"session_id"`
	}{{Agent: "tester", SessionID: "room-public-tester"}}, view.Room.Members, "the public room seats every agent of yours, the built-ins left out")

	longPost := strings.Repeat("a", 71) + "über the limit of one table row"
	room := roomsdomain.Room{Name: "public", Project: project}
	require.NoError(t, roomsinfra.NewJSONLLog().Append(room, roomsdomain.Post{Author: "user", Text: longPost}))

	out = runInfer(t, "rooms", "show", "public")
	require.True(t, utf8.ValidString(out), "a long post is cut on a character boundary:\n%q", out)
	require.Contains(t, out, "Log (1 posts)")
}

// serveDaemonRooms plays a running daemon whose public room runs in project,
// and points the commands' binding at it.
func serveDaemonRooms(t *testing.T, project string) {
	t.Helper()
	rooms, err := json.Marshal(map[string]any{"type": "rooms", "rooms": []map[string]any{{
		"name":    "public",
		"project": project,
		"members": []map[string]string{{"agent": "main", "session_id": "room-public-main", "activity": "idle"}},
	}}})
	require.NoError(t, err)
	handler := &aguimocks.FakeHandler{}
	handler.HandleCalls(func(conn *agui.Conn, frame []byte) {
		if strings.Contains(string(frame), `"list_rooms"`) {
			conn.Deliver(rooms)
		}
		if strings.Contains(string(frame), `"clear_room"`) {
			conn.Deliver([]byte(`{"type":"room_cleared","room":"public"}`))
		}
	})
	binding := agui.NewBinding(agui.BindingConfig{Port: 0, Token: testBindingToken, Handshake: browserinfra.BindingHandshake}, handler)
	require.NoError(t, binding.Start())
	t.Cleanup(binding.Close)
	_, port, err := net.SplitHostPort(binding.Addr())
	require.NoError(t, err)
	t.Setenv("INFER_DAEMON_BINDING_ENABLED", "true")
	t.Setenv("INFER_DAEMON_BINDING_PORT", port)
	t.Setenv("INFER_DAEMON_BINDING_TOKEN", testBindingToken)
}

// TestShowRoomReadsTheLogTheDaemonWrites pins that a room without a project
// resolves where the daemon runs, not where the command runs, so show prints
// the posts the daemon logged.
func TestShowRoomReadsTheLogTheDaemonWrites(t *testing.T) {
	isolate(t)
	daemonProject := t.TempDir()
	serveDaemonRooms(t, daemonProject)
	room := roomsdomain.Room{Name: "public", Project: daemonProject}
	require.NoError(t, roomsinfra.NewJSONLLog().Append(room, roomsdomain.Post{Author: "user", Text: "posted through the daemon"}))

	var view struct {
		Room struct {
			Project string `json:"project"`
		} `json:"room"`
		Posts []roomsdomain.Post `json:"posts"`
	}
	out := runInfer(t, "rooms", "show", "public", "--format", "json")
	require.NoError(t, json.Unmarshal([]byte(out), &view), out)
	require.Equal(t, daemonProject, view.Room.Project)
	require.Len(t, view.Posts, 1)
	require.Equal(t, "posted through the daemon", view.Posts[0].Text)
}

// TestClearRoomEmptiesTheLogHereWithoutADaemon pins the fallback: with no
// daemon answering, clear empties the log of the room as resolved here.
func TestClearRoomEmptiesTheLogHereWithoutADaemon(t *testing.T) {
	isolate(t)
	room := roomsdomain.Room{Name: "public", Project: projects.Path()}
	require.NoError(t, roomsinfra.NewJSONLLog().Append(room, roomsdomain.Post{Author: "user", Text: "hi all"}))

	out := runInfer(t, "rooms", "clear", "public")
	require.Contains(t, out, "Cleared #public.")
	posts, err := roomsinfra.NewJSONLLog().Read(room)
	require.NoError(t, err)
	require.Empty(t, posts)
}

// TestClearRoomGoesThroughTheRunningDaemon pins that a running daemon clears
// the room, so the command leaves the log it would resolve here alone.
func TestClearRoomGoesThroughTheRunningDaemon(t *testing.T) {
	isolate(t)
	serveDaemonRooms(t, t.TempDir())
	room := roomsdomain.Room{Name: "public", Project: projects.Path()}
	require.NoError(t, roomsinfra.NewJSONLLog().Append(room, roomsdomain.Post{Author: "user", Text: "kept here"}))

	out := runInfer(t, "rooms", "clear", "public")
	require.Contains(t, out, "Cleared #public.")
	posts, err := roomsinfra.NewJSONLLog().Read(room)
	require.NoError(t, err)
	require.Len(t, posts, 1, "the daemon cleared its own log, not this one")
}
