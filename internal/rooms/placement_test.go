package rooms

import (
	"errors"
	"testing"
	"testing/synctest"

	require "github.com/stretchr/testify/require"

	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// workspace is the computer the pinned agent's file names.
const workspace = "ubuntu-desktop"

// openWith plays the snapshot the member's thread opens with, holding count
// messages, as the registry answers a resume_conversation.
func openWith(t *testing.T, seat sessionsdomain.Client, count int) {
	t.Helper()
	messages := make([]map[string]string, count)
	for i := range messages {
		messages[i] = map[string]string{"id": "m", "role": "user", "content": "earlier"}
	}
	seat.Deliver(encode(map[string]any{"type": frameMessagesSnapshot, "messages": messages}))
	synctest.Wait()
}

func sentTypes(frames []map[string]any) []any {
	types := make([]any, 0, len(frames))
	for _, frame := range frames {
		types = append(types, frame["type"])
	}
	return types
}

// TestAPinnedMemberIsPlacedBeforeItsFirstTurn covers a member whose agent pins
// a workspace: its new session moves there before the turn goes out, and its
// later turns follow it there without being placed again.
func TestAPinnedMemberIsPlacedBeforeItsFirstTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		f.pin("tester", workspace)

		f.post(t, &sessionsmocks.FakeClient{}, "@tester run the suite")
		require.Equal(t, []any{sessionsdomain.FrameResumeConversation}, sentTypes(f.allSent(t)), "only the session opens before the placement")
		require.Empty(t, *f.placements.Load())

		openWith(t, f.seat(t, "tester"), 0)
		require.Equal(t, []string{"room-release-tester@" + workspace}, *f.placements.Load())
		require.Equal(t, agui.FrameRunAgentInput, f.sent(t)[len(f.sent(t))-1]["type"], "the turn goes out once the session landed")

		reply(t, f.seat(t, "tester"), "all green")
		f.post(t, &sessionsmocks.FakeClient{}, "@tester and again")
		require.Len(t, *f.placements.Load(), 1, "a placed member is not placed again")
		require.Equal(t, agui.FrameRunAgentInput, f.sent(t)[len(f.sent(t))-1]["type"])
	})
}

// TestAFailedPlacementNotesItAndHoldsThePost covers a placement that fails:
// the room notes why, the member goes idle without running, and the post waits
// for its next turn, which places it again.
func TestAFailedPlacementNotesItAndHoldsThePost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		f.pin("tester", workspace)
		f.placeErr.Store(new(errors.New("workspace unreachable")))
		follower := &sessionsmocks.FakeClient{}
		f.send(t, follower, map[string]string{"type": roomsdomain.FrameFollowRoom, "room": "release"})

		f.post(t, &sessionsmocks.FakeClient{}, "@tester run the suite")
		openWith(t, f.seat(t, "tester"), 0)

		logged := f.logged(t)
		require.Equal(t, roomsdomain.RoomAuthor, logged[len(logged)-1].Author)
		require.Equal(t, "tester could not reach its computer ubuntu-desktop: workspace unreachable", logged[len(logged)-1].Text)
		require.NotContains(t, sentTypes(f.allSent(t)), agui.FrameRunAgentInput, "the turn never ran")
		activity := delivered[activityFrame](t, follower, roomsdomain.FrameRoomActivity)
		require.Equal(t, roomsdomain.ActivityIdle, activity[len(activity)-1].Activity)

		f.placeErr.Store(nil)
		f.post(t, &sessionsmocks.FakeClient{}, "@tester and the lint")
		openWith(t, f.seat(t, "tester"), 0)
		require.Len(t, *f.placements.Load(), 2, "the held post's turn places the member again")
		require.Contains(t, turnText(t, f.sent(t)[len(f.sent(t))-1]), "user: tester run the suite", "the held post goes first")
	})
}

// TestASessionThatAlreadyAnsweredStaysWhereItRuns covers a member whose
// session held messages before its agent named a workspace: it is not moved.
func TestASessionThatAlreadyAnsweredStaysWhereItRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)
		f.pin("tester", workspace)

		f.post(t, &sessionsmocks.FakeClient{}, "@tester run the suite")
		openWith(t, f.seat(t, "tester"), 2)

		require.Empty(t, *f.placements.Load())
		require.Equal(t, agui.FrameRunAgentInput, f.sent(t)[len(f.sent(t))-1]["type"])
	})
}

// TestAnUnpinnedMemberIsNeverPlaced covers a member whose agent names no
// workspace: its turn goes out at once, with no placement.
func TestAnUnpinnedMemberIsNeverPlaced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		f := newHostFixture(t, nil)

		f.post(t, &sessionsmocks.FakeClient{}, "@tester run the suite")

		require.Empty(t, *f.placements.Load())
		require.Len(t, f.allSent(t), turnFrames)
	})
}
