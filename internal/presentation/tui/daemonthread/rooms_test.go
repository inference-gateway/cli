package daemonthread

import (
	"testing"

	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

const (
	testRoomsReply    = `{"type":"rooms","rooms":[{"name":"release","project":"/proj","members":[{"agent":"tester","session_id":"room-release-tester","activity":"running"}]}]}`
	testSnapshotReply = `{"type":"room_snapshot","room":{"name":"release","project":"/proj","members":[]},"posts":[{"id":"p1","author":"user","text":"hi","time":"2026-11-23T10:00:00Z"}]}`
	testPostedReply   = `{"type":"room_posted","room":"release","post":{"id":"p2","author":"user","text":"@tester go","mentions":["tester"],"routes":[{"member":"tester","probability":1}],"time":"2026-11-23T10:01:00Z"},"notices":["@reviewer is not in room release."]}`
)

func TestRoomsListJoinPostAndLeave(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	rooms := NewRooms(thread)
	d.answer(frameListRooms, testRoomsReply)
	d.answer(frameJoinRoom, testSnapshotReply)
	d.answer(framePostRoom, testPostedReply)

	listed, err := rooms.List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []roomsdomain.RoomStatus{{
		Name: "release", Project: "/proj",
		Members: []roomsdomain.MemberStatus{{Agent: "tester", SessionID: "room-release-tester", Activity: roomsdomain.ActivityRunning}},
	}}, listed)

	snapshot, err := rooms.Join(t.Context(), "release")
	require.NoError(t, err)
	require.Equal(t, "release", snapshot.Room.Name)
	require.Len(t, snapshot.Posts, 1)
	require.Equal(t, "release", d.frames(frameJoinRoom)[0]["room"])

	posted, err := rooms.Post(t.Context(), "release", "@tester go")
	require.NoError(t, err)
	require.Equal(t, "Routed to tester (pinged).", posted.RouteSummary())
	require.Equal(t, []string{"@reviewer is not in room release."}, posted.Notices)
	require.Equal(t, "@tester go", d.frames(framePostRoom)[0]["text"])

	require.NoError(t, rooms.Leave("release"))
	eventually(t, "the leave", func() bool { return len(d.frames(frameLeaveRoom)) == 1 })
}

func TestRoomsStreamPostsAndActivityToTheUI(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	d.answer(frameJoinRoom, testSnapshotReply)
	_, err := NewRooms(thread).Join(t.Context(), "release")
	require.NoError(t, err)

	send(d.conn(), `{"type":"room_post","room":"release","post":{"id":"p3","author":"tester","text":"all green","time":"2026-11-23T10:02:00Z"}}`)
	send(d.conn(), `{"type":"room_activity","room":"release","member":"tester","activity":"idle"}`)
	send(d.conn(), `{"type":"room_cleared","room":"release"}`)

	eventually(t, "the post, the activity and the clear", func() bool {
		return len(notified[tui.RoomPostEvent](notifier)) == 1 && len(notified[tui.RoomActivityEvent](notifier)) == 1 &&
			len(notified[tui.RoomClearedEvent](notifier)) == 1
	})
	post := notified[tui.RoomPostEvent](notifier)[0]
	require.Equal(t, "release", post.Room)
	require.Equal(t, "all green", post.Post.Text)
	require.Equal(t, tui.RoomActivityEvent{Room: "release", Member: "tester", Activity: roomsdomain.ActivityIdle}, notified[tui.RoomActivityEvent](notifier)[0])
}

// TestRoomsFollowTheRoomAgainAfterAReconnect pins that a reconnect follows the
// joined room again without a second welcome and hands the UI its snapshot.
func TestRoomsFollowTheRoomAgainAfterAReconnect(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	d.answer(frameJoinRoom, testSnapshotReply)
	_, err := NewRooms(thread).Join(t.Context(), "release")
	require.NoError(t, err)

	d.binding.Close()
	again := startDaemon(t, d.port)
	again.answer(frameFollowRoom, testSnapshotReply)

	eventually(t, "the room's snapshot again", func() bool { return len(notified[tui.RoomSnapshotEvent](notifier)) == 1 })
	require.Len(t, again.frames(frameFollowRoom), 1)
	require.Empty(t, again.frames(frameJoinRoom), "a reconnect never welcomes")
}

// TestSessionlessThreadReachesOnlyARunningDaemon pins the plain chat's rooms
// connection: it dials on first use, reports no placement when the daemon
// goes, and fails instead of starting a daemon that is not running.
func TestSessionlessThreadReachesOnlyARunningDaemon(t *testing.T) {
	d := startDaemon(t, 0)
	thread := NewSessionless(d.config())
	t.Cleanup(thread.Close)
	notifier := &agentdomainmocks.FakeUINotifier{}
	thread.SetNotifier(notifier)
	d.answer(frameListRooms, testRoomsReply)
	d.answer(frameJoinRoom, testSnapshotReply)
	rooms := NewRooms(thread)

	listed, err := rooms.List(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	_, err = rooms.Join(t.Context(), "release")
	require.NoError(t, err)

	d.binding.Close()
	eventually(t, "the lost-daemon status", func() bool { return len(notified[tui.SetStatusEvent](notifier)) == 1 })
	require.Empty(t, notified[tui.SessionPlacementChangedEvent](notifier), "a plain chat runs here, wherever the daemon is")

	stopped := NewSessionless(d.config())
	t.Cleanup(stopped.Close)
	_, err = NewRooms(stopped).List(t.Context())
	require.Error(t, err, "nothing listens, and a plain chat starts no daemon")
}
