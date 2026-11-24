package domain

import "slices"

// UserAuthor writes the posts a client posts to a room, and RoomAuthor the
// notes the room logs about its members, such as one waiting on an approval.
const (
	UserAuthor = "user"
	RoomAuthor = "room"
)

// FirstVisit reports whether the user has never been in the room whose log is
// history: it holds no post of theirs, not even a join.
func FirstVisit(history []Post) bool {
	return !slices.ContainsFunc(history, func(post Post) bool { return post.Author == UserAuthor })
}

// InvokedByRoom is what a member's session records as having invoked it.
const InvokedByRoom = "room"

// Activity is what a member is doing in its room: running a turn, waiting on
// an approval in its own chat, or idle.
type Activity string

const (
	ActivityIdle    Activity = "idle"
	ActivityRunning Activity = "running"
	ActivityWaiting Activity = "waiting"
)

// UnknownSeen is the seen index of a member whose last turn this daemon has
// not delivered, so its turn starts after its own last post in the log.
const UnknownSeen = -1

// Unseen returns the posts of history a member has not seen: those from index
// seen on, the last limit of them, with the member's own posts and the post
// addressed to it left out. With seen UnknownSeen they start after the
// member's own last post.
func Unseen(history []Post, member string, seen int, addressedID string, limit int) []Post {
	from := seen
	if from == UnknownSeen {
		from = lastPostBy(history, member) + 1
	}
	unseen := []Post{}
	for _, post := range history[min(from, len(history)):] {
		if post.Author != member && post.ID != addressedID {
			unseen = append(unseen, post)
		}
	}
	return unseen[max(0, len(unseen)-limit):]
}

func lastPostBy(history []Post, author string) int {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Author == author {
			return i
		}
	}
	return -1
}
