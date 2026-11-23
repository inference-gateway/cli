package rooms

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/synctest"

	require "github.com/stretchr/testify/require"

	decisionmocks "github.com/inference-gateway/cli/tests/mocks/decision"
	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
	roomsinfra "github.com/inference-gateway/cli/internal/rooms/infrastructure"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// hostFixture is one daemon's rooms under test: the registry and the decider
// are fakes, the log is the real JSONL log in a temporary home.
type hostFixture struct {
	host    *Host
	threads *sessionsmocks.FakeThreadRouter
	decider *decisionmocks.FakeDecider
	room    roomsdomain.Room
}

func newHostFixture(t *testing.T, mutate func(*config.RoomsConfig)) hostFixture {
	t.Helper()
	project := t.TempDir()
	cfg := *config.DefaultRoomsConfig()
	cfg.Rooms = []config.RoomConfig{{Name: "release", Project: project, Agents: []string{"tester", "writer"}}}
	if mutate != nil {
		mutate(&cfg)
	}
	threads := &sessionsmocks.FakeThreadRouter{}
	decider := &decisionmocks.FakeDecider{}
	agents := func(string) []agentdomain.SubagentInfo { return testAgents }
	host := NewHost(t.Context(), cfg, threads, roomsinfra.NewJSONLLog(), decider, agents)
	return hostFixture{host: host, threads: threads, decider: decider, room: roomsdomain.Room{Name: "release", Project: project, Agents: []string{"tester", "writer"}}}
}

func (f hostFixture) send(t *testing.T, c sessionsdomain.Client, frame map[string]string) {
	t.Helper()
	require.True(t, f.host.Handle(c, encode(frame)), "the host claims %s", frame["type"])
	synctest.Wait()
}

func (f hostFixture) post(t *testing.T, c sessionsdomain.Client, text string) {
	t.Helper()
	f.send(t, c, map[string]string{"type": framePostRoom, "room": "release", "text": text})
}

// sent decodes the frames the host sent the registry, in order.
func (f hostFixture) sent(t *testing.T) []map[string]any {
	t.Helper()
	frames := make([]map[string]any, 0, f.threads.HandleCallCount())
	for i := range f.threads.HandleCallCount() {
		_, raw := f.threads.HandleArgsForCall(i)
		var frame map[string]any
		require.NoError(t, json.Unmarshal(raw, &frame))
		frames = append(frames, frame)
	}
	return frames
}

// seat is the member client of agent, as the host handed it to the registry.
func (f hostFixture) seat(t *testing.T, agent string) sessionsdomain.Client {
	t.Helper()
	for i := range f.threads.HandleCallCount() {
		client, _ := f.threads.HandleArgsForCall(i)
		if m, ok := client.(*member); ok && m.agent == agent {
			return m
		}
	}
	t.Fatalf("no turn was delivered to %s", agent)
	return nil
}

// reply plays a worker's run that answers text.
func reply(t *testing.T, seat sessionsdomain.Client, text string) {
	t.Helper()
	for _, frame := range [][]byte{
		[]byte(`{"type":"RUN_STARTED"}`),
		[]byte(`{"type":"TEXT_MESSAGE_START","role":"assistant"}`),
		encode(map[string]string{"type": "TEXT_MESSAGE_CONTENT", "delta": text}),
		[]byte(`{"type":"TEXT_MESSAGE_END"}`),
		[]byte(`{"type":"RUN_FINISHED"}`),
	} {
		seat.Deliver(frame)
	}
	synctest.Wait()
}

func (f hostFixture) logged(t *testing.T) []roomsdomain.Post {
	t.Helper()
	posts, err := roomsinfra.NewJSONLLog().Read(f.room)
	require.NoError(t, err)
	return posts
}

// delivered decodes every frame of type typ the client got.
func delivered[T any](t *testing.T, c *sessionsmocks.FakeClient, typ string) []T {
	t.Helper()
	var frames []T
	for i := range c.DeliverCallCount() {
		raw := c.DeliverArgsForCall(i)
		var head struct {
			Type string `json:"type"`
		}
		require.NoError(t, json.Unmarshal(raw, &head))
		if head.Type != typ {
			continue
		}
		var frame T
		require.NoError(t, json.Unmarshal(raw, &frame))
		frames = append(frames, frame)
	}
	return frames
}

func turnText(t *testing.T, frame map[string]any) string {
	t.Helper()
	input, _ := frame["input"].(map[string]any)
	messages, _ := input["messages"].([]any)
	require.Len(t, messages, 1)
	content, _ := messages[0].(map[string]any)["content"].(string)
	return content
}

// TestPostReachesTheMemberAndLogsItsReply covers a post from routing to the
// logged reply: the poster learns the routes, the member gets the post as its
// next turn on its own session, and its answer lands in the log.
func TestPostReachesTheMemberAndLogsItsReply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		poster := &sessionsmocks.FakeClient{}

		f.post(t, poster, "@tester run the suite")
		posted := delivered[postedFrame](t, poster, frameRoomPosted)
		require.Len(t, posted, 1)
		require.Equal(t, []roomsdomain.Route{{Member: "tester", Probability: 1}}, posted[0].Post.Routes)

		sent := f.sent(t)
		require.Len(t, sent, 2, "one resume_conversation and one run_agent_input")
		require.Equal(t, frameResume, sent[0]["type"])
		require.Equal(t, "room-release-tester", sent[0]["id"])
		require.Equal(t, f.room.Project, sent[0]["project_dir"])
		require.Equal(t, "tester", sent[0]["agent"])
		require.Equal(t, map[string]any{"agent": "tester", "label": "release", "invoked_by": "room"}, sent[0]["origin"])
		require.Equal(t, frameRunInput, sent[1]["type"])
		require.Contains(t, turnText(t, sent[1]), "The post for you:\nuser: tester run the suite", "a member's name reaches it without the @ file expansion reads")

		reply(t, f.seat(t, "tester"), "all green")
		logged := f.logged(t)
		require.Len(t, logged, 2)
		require.Equal(t, "tester", logged[1].Author)
		require.Equal(t, "all green", logged[1].Text)
	})
}

func TestMembersRoutedTheSamePostRunInParallel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		f.post(t, &sessionsmocks.FakeClient{}, "@tester @writer cut the release")

		turns := map[string]bool{}
		for _, frame := range f.sent(t) {
			if frame["type"] == frameResume {
				turns[frame["agent"].(string)] = true
			}
		}
		require.Equal(t, map[string]bool{"tester": true, "writer": true}, turns, "both turns start before either member answers")
	})
}

// TestAPostForABusyMemberQueuesAsItsNextTurn pins the hold: a post for a
// member mid-turn waits for the turn to end, and then carries the posts the
// member has not seen since.
func TestAPostForABusyMemberQueuesAsItsNextTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		poster := &sessionsmocks.FakeClient{}

		f.post(t, poster, "@tester run the suite")
		f.post(t, poster, "@writer draft the notes")
		f.post(t, poster, "@tester and the e2e suite")
		require.Equal(t, 4, f.threads.HandleCallCount(), "the second post for tester waits while it runs")

		reply(t, f.seat(t, "writer"), "notes drafted")
		reply(t, f.seat(t, "tester"), "unit tests pass")
		sent := f.sent(t)
		require.Len(t, sent, 6, "tester's turn ended, so its held post goes out")
		next := turnText(t, sent[5])
		require.Contains(t, next, "Posts since your last turn:\nuser: writer draft the notes\nwriter: notes drafted\n")
		require.Contains(t, next, "The post for you:\nuser: tester and the e2e suite")
		require.NotContains(t, next, "unit tests pass", "the member's own reply is not news to it")
	})
}

// TestAnApprovalWaitsInTheMembersOwnChat pins that the room never answers a
// member's approval: it notes the wait, holds the member's next post, and logs
// the answer once the user decided in the member's chat.
func TestAnApprovalWaitsInTheMembersOwnChat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		poster := &sessionsmocks.FakeClient{}
		f.send(t, poster, map[string]string{"type": frameFollowRoom, "room": "release"})

		f.post(t, poster, "@tester deploy it")
		seat := f.seat(t, "tester")
		seat.Deliver([]byte(`{"type":"RUN_FINISHED","outcome":{"type":"interrupt","interrupts":[{"id":"i1","reason":"tool_call"}]}}`))
		synctest.Wait()

		logged := f.logged(t)
		require.Equal(t, roomsdomain.RoomAuthor, logged[len(logged)-1].Author)
		require.Contains(t, logged[len(logged)-1].Text, "tester waits for approval in its own chat")
		activity := delivered[activityFrame](t, poster, frameRoomActivity)
		require.Equal(t, roomsdomain.ActivityWaiting, activity[len(activity)-1].Activity)

		f.post(t, poster, "@tester also tag the release")
		require.Equal(t, 2, f.threads.HandleCallCount(), "the room sends no resume entry and holds the next post")

		reply(t, seat, "deployed")
		logged = f.logged(t)
		require.Contains(t, []string{logged[len(logged)-1].Text, logged[len(logged)-2].Text}, "deployed")
		require.Equal(t, 4, f.threads.HandleCallCount(), "the held post goes out once the approved run finished")
	})
}

func TestFollowStreamsTheRoomUntilLeave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		poster := &sessionsmocks.FakeClient{}
		follower := &sessionsmocks.FakeClient{}

		f.post(t, poster, "@tester before the follow")
		f.send(t, follower, map[string]string{"type": frameFollowRoom, "room": "release"})
		snapshots := delivered[snapshotFrame](t, follower, frameRoomSnapshot)
		require.Len(t, snapshots, 1)
		require.Len(t, snapshots[0].Posts, 1, "the snapshot carries the log so far")
		require.Equal(t, roomsdomain.ActivityRunning, snapshots[0].Room.Members[0].Activity, "tester is mid-turn")

		reply(t, f.seat(t, "tester"), "on it")
		require.Len(t, delivered[postFrame](t, follower, frameRoomPost), 1, "the reply streams to the follower")
		require.NotEmpty(t, delivered[activityFrame](t, follower, frameRoomActivity))

		f.send(t, follower, map[string]string{"type": frameLeaveRoom, "room": "release"})
		f.post(t, poster, "@writer after the leave")
		require.Len(t, delivered[postFrame](t, follower, frameRoomPost), 1, "nothing streams after leave_room")

		f.send(t, follower, map[string]string{"type": frameFollowRoom, "room": "release"})
		f.host.Detach(follower)
		f.post(t, poster, "@writer after the disconnect")
		require.Len(t, delivered[postFrame](t, follower, frameRoomPost), 1, "nothing streams to a disconnected client")
	})
}

func TestListRoomsAnswersEveryRoomWithItsMembers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		client := &sessionsmocks.FakeClient{}
		f.send(t, client, map[string]string{"type": frameListRooms})

		frames := delivered[roomsFrame](t, client, frameRooms)
		require.Len(t, frames, 1)
		names := make([]string, 0, len(frames[0].Rooms))
		for _, room := range frames[0].Rooms {
			names = append(names, room.Name)
		}
		require.Equal(t, []string{"release"}, names)
		require.Equal(t, memberView{Agent: "tester", SessionID: "room-release-tester", Activity: roomsdomain.ActivityIdle}, frames[0].Rooms[0].Members[0])
	})
}

func TestPostToAnUnknownRoomFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		client := &sessionsmocks.FakeClient{}
		f.send(t, client, map[string]string{"type": framePostRoom, "room": "nowhere", "text": "hello"})

		errs := delivered[map[string]string](t, client, frameRunError)
		require.Len(t, errs, 1)
		require.Contains(t, errs[0]["message"], `room "nowhere" not found`)
	})
}

// TestAFailedRoutingStillLogsThePost pins that the user's words survive a
// decision request that failed: the post is logged routed to nobody, and the
// poster learns why.
func TestAFailedRoutingStillLogsThePost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, func(cfg *config.RoomsConfig) { cfg.Router.Model = testRouterModel })
		f.decider.DecideReturns(probabilities(nil), errors.New("the Decisions API is not enabled"))
		poster := &sessionsmocks.FakeClient{}

		f.post(t, poster, "who can help")
		posted := delivered[postedFrame](t, poster, frameRoomPosted)
		require.Len(t, posted, 1)
		require.Empty(t, posted[0].Post.Routes)
		require.True(t, strings.Contains(strings.Join(posted[0].Notices, " "), "routing failed"))
		require.Len(t, f.logged(t), 1)
		require.Zero(t, f.threads.HandleCallCount())
	})
}
