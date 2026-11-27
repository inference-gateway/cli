package components

import (
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

var roomStart = time.Date(2026, 11, 23, 10, 0, 0, 0, time.Local)

func newTestRoomView(posts ...roomsdomain.Post) *RoomView {
	view := NewRoomView(styles.NewProvider(styles.NewThemeProvider()))
	view.SetWidth(80)
	view.SetHeight(40)
	view.Show(roomsdomain.Snapshot{
		Room: roomsdomain.RoomStatus{Name: "release", Members: []roomsdomain.MemberStatus{
			{Agent: "tester", Role: "qa", Activity: roomsdomain.ActivityIdle},
			{Agent: "writer", Activity: roomsdomain.ActivityRunning},
		}},
		Posts: posts,
	})
	return view
}

// roomScreen is the room view's visible text without colors or the padding the
// viewport fills each line with.
func roomScreen(view *RoomView) string {
	lines := strings.Split(plain(view.Render()), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func roomPost(author string, after time.Duration, text string) roomsdomain.Post {
	return roomsdomain.Post{Author: author, Text: text, Time: roomStart.Add(after)}
}

func TestRoomViewDrawsTheChannelLikeSlack(t *testing.T) {
	join := roomPost(roomsdomain.UserAuthor, 0, "joined #release")
	join.Kind = roomsdomain.PostKindJoin
	hello := roomPost(roomsdomain.UserAuthor, time.Minute, "hi @tester")
	hello.Mentions = []string{"tester"}
	view := newTestRoomView(
		join,
		hello,
		roomPost("tester", 2*time.Minute, "hey!"),
		roomPost(roomsdomain.RoomAuthor, 3*time.Minute, "writer waits for approval in its own chat"),
	)

	out := roomScreen(view)
	for _, want := range []string{
		"# release · 2 members  ○ tester (qa) idle · ● writer working",
		"→ you joined #release",
		"you  10:01\nhi @tester",
		"tester  10:02",
		"hey!",
		"writer waits for approval in its own chat",
		"writer is working…",
	} {
		require.Contains(t, out, want)
	}
	require.NotContains(t, out, "tester is", "an idle member is not listed as busy")
}

// TestRoomViewGroupsAnAuthorsMessages pins Slack's grouping: a message soon
// after the same author's last one shares its author line, a later one or
// one after a join starts its own.
func TestRoomViewGroupsAnAuthorsMessages(t *testing.T) {
	view := newTestRoomView(
		roomPost("tester", 0, "first"),
		roomPost("tester", time.Minute, "second"),
		roomPost("tester", time.Minute+groupWindow+time.Second, "much later"),
	)

	out := roomScreen(view)
	require.Equal(t, 2, strings.Count(out, "tester  "), "the second message shares the first one's author line")
	require.Contains(t, out, "tester  10:06")
}

func TestRoomViewFollowsNewPostsAndActivity(t *testing.T) {
	view := newTestRoomView()
	view.SetHeight(6)
	for i := range 10 {
		view.AddPost(roomPost("tester", time.Duration(i)*groupWindow*2, "post "+string(rune('a'+i))))
	}
	require.Contains(t, roomScreen(view), "post j", "the view stays on the newest post")

	view.SetActivity("writer", roomsdomain.ActivityWaiting)
	require.Contains(t, roomScreen(view), "writer is waiting for approval")
	require.Contains(t, roomScreen(view), "◐ writer waiting for approval", "the pinned header shows each member's presence")

	view.Update(tui.ScrollRequestEvent{ComponentID: "conversation", Direction: tui.ScrollToTop})
	view.AddPost(roomPost("tester", time.Hour*3, "newest"))
	require.NotContains(t, roomScreen(view), "newest", "a user who scrolled up keeps their place")
	require.Equal(t, 6, strings.Count(view.Render(), "\n")+1, "the room fills exactly its height")
}

// TestRoomViewShowsAMembersCallWaitingForYou pins the approval post: the call
// and the note it came with, no choices of its own since the chat's approval
// window decides it, and the newest open call moving on once one is decided.
func TestRoomViewShowsAMembersCallWaitingForYou(t *testing.T) {
	asked := func(id, tool string, after time.Duration) roomsdomain.Post {
		post := roomPost("tester", after, "Rejected by judge: tests were not asked for")
		post.ID, post.Kind = "post-"+id, roomsdomain.PostKindApproval
		post.Approval = &roomsdomain.Approval{ID: id, Tool: tool, Arguments: `{"command":"go test ./..."}`}
		return post
	}
	view := newTestRoomView(asked("c1", "Bash", 0), asked("c2", "Write", time.Minute))

	screen := roomScreen(view)
	require.Contains(t, screen, `wants to run Bash {"command":"go test ./..."}`)
	require.Contains(t, screen, "Rejected by judge: tests were not asked for")
	require.NotContains(t, screen, "Approve", "the approval window decides the call, not the room")
	newest, ok := view.NewestOpenApproval()
	require.True(t, ok)
	require.Equal(t, "c2", newest.Approval.ID)

	decided := roomPost(roomsdomain.RoomAuthor, 2*time.Minute, "You approved tester's Write call.")
	decided.Kind, decided.Approval = roomsdomain.PostKindDecision, &roomsdomain.Approval{ID: "c2", Decision: roomsdomain.DecisionApprove}
	view.AddPost(decided)
	require.Contains(t, roomScreen(view), "You approved tester's Write call.")
	newest, _ = view.NewestOpenApproval()
	require.Equal(t, "c1", newest.Approval.ID, "the call still open is the next one to decide")
}

// TestRoomViewRendersAMembersMessageOnce pins the render cache: a member's
// message is kept by its post id, so an activity redraws the room without
// rendering the log again, and a new width renders it afresh.
func TestRoomViewRendersAMembersMessageOnce(t *testing.T) {
	answer := roomPost("tester", 0, "all green")
	answer.ID = "p1"
	view := newTestRoomView(answer, roomPost("tester", time.Hour, "a post of an older log, without an id"))
	require.Len(t, view.renderedBodies, 1, "only a post with an id is kept")

	view.renderedBodies[answer.ID] = "kept body"
	view.SetActivity("writer", roomsdomain.ActivityIdle)
	require.Contains(t, roomScreen(view), "kept body", "an activity redraws the room from the kept body")

	view.SetWidth(60)
	require.NotContains(t, roomScreen(view), "kept body", "a new width renders the message again")
	require.Contains(t, roomScreen(view), "all green")
}

// TestRoomViewHeaderShowsWhatTheRoomSpent pins the room's spend in the pinned
// header: what the posts of its log cost, routing and member turns, and
// nothing while the room spent nothing or once its log was cleared.
func TestRoomViewHeaderShowsWhatTheRoomSpent(t *testing.T) {
	view := newTestRoomView()
	require.NotContains(t, roomScreen(view), "tokens", "a room that spent nothing says nothing about it")

	view.AddPost(roomsdomain.Post{Author: roomsdomain.UserAuthor, Text: "hi all", Usage: roomsdomain.Usage{Tokens: 300}, Time: roomStart})
	view.AddPost(roomsdomain.Post{Author: "tester", Text: "hi", Usage: roomsdomain.Usage{Tokens: 1500, Cost: 0.0123}, Time: roomStart})
	require.Contains(t, roomScreen(view), "# release · 2 members · 1.8k tokens · $0.012")

	view.ClearPosts()
	require.NotContains(t, roomScreen(view), "tokens", "a cleared room starts over")
}
