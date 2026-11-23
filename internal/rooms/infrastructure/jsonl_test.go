package infrastructure

import (
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

func testRoom(project string) roomsdomain.Room {
	return roomsdomain.Room{Name: "public", Project: project}
}

func TestJSONLLogAppendAndRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	log := NewJSONLLog()
	room := testRoom(t.TempDir())

	stamped := time.Date(2026, 11, 23, 10, 30, 0, 0, time.UTC)
	first := roomsdomain.Post{
		Author:   "@edenreich",
		Text:     "prepare the release",
		Mentions: []string{"researcher", "tester"},
		Routes:   []roomsdomain.Route{{Member: "researcher", Probability: 0.91}},
		Time:     stamped,
	}
	require.NoError(t, log.Append(room, first))

	second := roomsdomain.Post{Author: "tester", Text: "tests pass"}
	require.NoError(t, log.Append(room, second))

	posts, err := log.Read(room)
	require.NoError(t, err)
	require.Len(t, posts, 2)
	require.Equal(t, first, posts[0], "the post reads back exactly as it was written")
	require.False(t, posts[1].Time.IsZero(), "a post written without a time is stamped")
	require.Equal(t, "tester", posts[1].Author)

	path := filepath.Join(home, config.ConfigDirName, config.ProjectsDirName, config.ProjectSlug(room.Project), "rooms", room.Name+".jsonl")
	require.FileExists(t, path, "the log lives at ~/.infer/projects/<slug of the room's project>/rooms/<room>.jsonl")
}

// TestJSONLLogScopesByRoomProject pins that two rooms carrying the same name
// but different projects keep separate logs.
func TestJSONLLogScopesByRoomProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	log := NewJSONLLog()

	first := testRoom(t.TempDir())
	second := testRoom(t.TempDir())
	require.NoError(t, log.Append(first, roomsdomain.Post{Author: "user", Text: "hello"}))

	posts, err := log.Read(second)
	require.NoError(t, err)
	require.Empty(t, posts, "the other project's room has no posts")

	posts, err = log.Read(first)
	require.NoError(t, err)
	require.Len(t, posts, 1)
}

func TestJSONLLogRejectsUnsafeRoomName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	log := NewJSONLLog()

	room := roomsdomain.Room{Name: "../evil", Project: t.TempDir()}
	_, err := log.Read(room)
	require.ErrorContains(t, err, "invalid room name")
	require.ErrorContains(t, log.Append(room, roomsdomain.Post{Author: "user"}), "invalid room name")
}
