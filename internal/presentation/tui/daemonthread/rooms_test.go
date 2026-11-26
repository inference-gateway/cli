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
	d.answer(roomsdomain.FrameListRooms, testRoomsReply)
	d.answer(roomsdomain.FrameJoinRoom, testSnapshotReply)
	d.answer(roomsdomain.FramePostRoom, testPostedReply)

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
	require.Equal(t, "release", d.frames(roomsdomain.FrameJoinRoom)[0]["room"])

	posted, err := rooms.Post(t.Context(), "release", "@tester go")
	require.NoError(t, err)
	require.Equal(t, "Routed to tester (pinged).", posted.RouteSummary())
	require.Equal(t, []string{"@reviewer is not in room release."}, posted.Notices)
	require.Equal(t, "@tester go", d.frames(roomsdomain.FramePostRoom)[0]["text"])

	require.NoError(t, rooms.Leave("release"))
	eventually(t, "the leave", func() bool { return len(d.frames(roomsdomain.FrameLeaveRoom)) == 1 })
}

func TestRoomsStreamPostsAndActivityToTheUI(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	d.answer(roomsdomain.FrameJoinRoom, testSnapshotReply)
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

// TestRoomsDecideAMembersCall pins the decision frame and its two answers: the
// daemon's confirmation, and its refusal of a call already decided.
func TestRoomsDecideAMembersCall(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	rooms := NewRooms(thread)
	d.answer(roomsdomain.FrameDecideRoomApproval, `{"type":"room_approval_decided","room":"release","member":"tester","tool_call_id":"c1","decision":"approve"}`)

	require.NoError(t, rooms.Decide(t.Context(), "release", "tester", "c1", roomsdomain.DecisionApprove))
	sent := d.frames(roomsdomain.FrameDecideRoomApproval)[0]
	require.Equal(t, map[string]any{"type": "decide_room_approval", "room": "release", "member": "tester", "tool_call_id": "c1", "decision": "approve"}, sent)

	d.answer(roomsdomain.FrameDecideRoomApproval, `{"type":"RUN_ERROR","message":"tester's call was already decided"}`)
	require.EqualError(t, rooms.Decide(t.Context(), "release", "tester", "c1", roomsdomain.DecisionReject), "tester's call was already decided")
}

// TestRoomsFollowTheRoomAgainAfterAReconnect pins that a reconnect follows the
// joined room again without a second welcome and hands the UI its snapshot.
func TestRoomsFollowTheRoomAgainAfterAReconnect(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	d.answer(roomsdomain.FrameJoinRoom, testSnapshotReply)
	_, err := NewRooms(thread).Join(t.Context(), "release")
	require.NoError(t, err)

	d.binding.Close()
	again := startDaemon(t, d.port)
	again.answer(roomsdomain.FrameFollowRoom, testSnapshotReply)

	eventually(t, "the room's snapshot again", func() bool { return len(notified[tui.RoomSnapshotEvent](notifier)) == 1 })
	require.Len(t, again.frames(roomsdomain.FrameFollowRoom), 1)
	require.Empty(t, again.frames(roomsdomain.FrameJoinRoom), "a reconnect never welcomes")
}

// TestSessionlessThreadSaysTheRoomIsBack pins a plain chat's status across a
// daemon restart: lost while the daemon is gone, back once the room is followed
// again, so the lost line never outlives the outage.
func TestSessionlessThreadSaysTheRoomIsBack(t *testing.T) {
	d := startDaemon(t, 0)
	thread := NewSessionless(d.config())
	t.Cleanup(thread.Close)
	notifier := &agentdomainmocks.FakeUINotifier{}
	thread.SetNotifier(notifier)
	d.answer(roomsdomain.FrameJoinRoom, testSnapshotReply)
	_, err := NewRooms(thread).Join(t.Context(), "release")
	require.NoError(t, err)

	d.binding.Close()
	again := startDaemon(t, d.port)
	again.answer(roomsdomain.FrameFollowRoom, testSnapshotReply)

	eventually(t, "the lost status, then the back one", func() bool { return len(notified[tui.SetStatusEvent](notifier)) == 2 })
	require.Equal(t, "#release is back", notified[tui.SetStatusEvent](notifier)[1].Message)
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
	d.answer(roomsdomain.FrameListRooms, testRoomsReply)
	d.answer(roomsdomain.FrameJoinRoom, testSnapshotReply)
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

// TestSessionlessThreadSaysOnceThatTheDaemonRunsAnotherBuild pins the warning a
// chat gives when the daemon it joins is another build: once, on the first
// dial, and never for a daemon of its own build.
func TestSessionlessThreadSaysOnceThatTheDaemonRunsAnotherBuild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	other := startDaemonOfBuild(t, 0, "another-build")
	thread := NewSessionless(other.config())
	t.Cleanup(thread.Close)
	notifier := &agentdomainmocks.FakeUINotifier{}
	thread.SetNotifier(notifier)
	other.answer(roomsdomain.FrameListRooms, testRoomsReply)
	rooms := NewRooms(thread)

	for range 2 {
		_, err := rooms.List(t.Context())
		require.NoError(t, err)
	}
	warnings := notified[tui.ShowErrorEvent](notifier)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0].Error, "another build")

	same := startDaemon(t, 0)
	quiet := NewSessionless(same.config())
	t.Cleanup(quiet.Close)
	quietNotifier := &agentdomainmocks.FakeUINotifier{}
	quiet.SetNotifier(quietNotifier)
	same.answer(roomsdomain.FrameListRooms, testRoomsReply)
	_, err := NewRooms(quiet).List(t.Context())
	require.NoError(t, err)
	require.Empty(t, notified[tui.ShowErrorEvent](quietNotifier))
}

// TestRoomsStopMembers pins the stop frame and its answer, the members stopped.
func TestRoomsStopMembers(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	rooms := NewRooms(thread)
	d.answer(roomsdomain.FrameStopRoom, `{"type":"room_stopped","room":"release","members":["tester"]}`)

	stopped, err := rooms.Stop(t.Context(), "release", "tester")
	require.NoError(t, err)
	require.Equal(t, []string{"tester"}, stopped)
	require.Equal(t, map[string]any{"type": "stop_room", "room": "release", "member": "tester"}, d.frames(roomsdomain.FrameStopRoom)[0])

	d.answer(roomsdomain.FrameStopRoom, `{"type":"room_stopped","room":"release","members":[]}`)
	stopped, err = rooms.Stop(t.Context(), "release", "")
	require.NoError(t, err)
	require.Empty(t, stopped)
}
