package rooms_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	require "github.com/stretchr/testify/require"

	root "github.com/inference-gateway/cli/cmd/root"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
	roomsinfra "github.com/inference-gateway/cli/internal/rooms/infrastructure"
)

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
	require.Contains(t, view.Room.Members, struct {
		Agent     string `json:"agent"`
		SessionID string `json:"session_id"`
	}{Agent: "main", SessionID: "room-public-main"}, "the public room seats every agent, built-ins included")

	longPost := strings.Repeat("a", 71) + "über the limit of one table row"
	room := roomsdomain.Room{Name: "public", Project: project}
	require.NoError(t, roomsinfra.NewJSONLLog().Append(room, roomsdomain.Post{Author: "user", Text: longPost}))

	out = runInfer(t, "rooms", "show", "public")
	require.True(t, utf8.ValidString(out), "a long post is cut on a character boundary:\n%q", out)
	require.Contains(t, out, "Log (1 posts)")
}
