package infrastructure

import (
	"os"
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

func TestJSONLLogReadsAnUnwrittenLogAsNoPosts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	posts, err := NewJSONLLog().Read(testRoom(t.TempDir()))
	require.NoError(t, err)
	require.NotNil(t, posts, "an unwritten log reads as an empty list, so its JSON is [] and not null")
	require.Empty(t, posts)
}

// TestJSONLLogSurvivesATornLine pins that a write cut short by a crash costs
// only the line it tore, never the rest of the room's log.
func TestJSONLLogSurvivesATornLine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	log := NewJSONLLog()
	room := testRoom(t.TempDir())

	require.NoError(t, log.Append(room, roomsdomain.Post{Author: "user", Text: "before"}))
	path, err := log.path(room)
	require.NoError(t, err)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = file.WriteString(`{"author":"user","text":"torn`)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, log.Append(room, roomsdomain.Post{Author: "user", Text: "glued to the torn line"}))
	require.NoError(t, log.Append(room, roomsdomain.Post{Author: "user", Text: "after"}))

	posts, err := log.Read(room)
	require.NoError(t, err)
	texts := make([]string, 0, len(posts))
	for _, post := range posts {
		texts = append(texts, post.Text)
	}
	require.Equal(t, []string{"before", "after"}, texts)
}
