package tui

import (
	"context"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// Rooms reaches the rooms the local daemon hosts, whichever way the chat runs.
// Join follows a room and, on the user's first visit, has its members welcome
// them. The room's posts and member activity then arrive as UI events. Decide
// answers a member's tool call waiting in the room.
type Rooms interface {
	List(ctx context.Context) ([]roomsdomain.RoomStatus, error)
	Join(ctx context.Context, room string) (roomsdomain.Snapshot, error)
	Post(ctx context.Context, room, text string) (roomsdomain.Posted, error)
	Decide(ctx context.Context, room, member, toolCallID string, decision roomsdomain.Decision) error
	Leave(room string) error
}

// RoomSnapshotEvent carries a followed room and its log so far, again after the
// daemon connection came back.
type RoomSnapshotEvent struct {
	Snapshot roomsdomain.Snapshot
}

// RoomPostEvent carries one new post of a followed room.
type RoomPostEvent struct {
	Room string
	Post roomsdomain.Post
}

// RoomClearedEvent says a followed room's log was emptied.
type RoomClearedEvent struct {
	Room string
}

// RoomApprovalAnsweredEvent is the answer the approval window gave to a room
// member's call, which the chat sends on to the room.
type RoomApprovalAnsweredEvent struct {
	ToolCallID string
	Action     agentdomain.ApprovalAction
}

// RoomActivityEvent carries what a member of a followed room does now, and the
// agent mode its turn runs in when the daemon said.
type RoomActivityEvent struct {
	Room     string
	Member   string
	Activity roomsdomain.Activity
	Mode     string
}
