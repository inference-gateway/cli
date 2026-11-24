package daemonthread

import (
	"context"
	"encoding/json"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// Rooms reaches the daemon's rooms over a thread connection: a daemon chat's
// own, or a sessionless one a plain chat dials on first use.
type Rooms struct {
	thread *Thread
}

// NewRooms builds the rooms client over thread.
func NewRooms(thread *Thread) *Rooms {
	return &Rooms{thread: thread}
}

// List asks the daemon for its rooms and what their members do.
func (r *Rooms) List(ctx context.Context) ([]roomsdomain.RoomStatus, error) {
	if err := r.thread.dialLazily(ctx); err != nil {
		return nil, err
	}
	data, err := r.thread.Request(ctx, map[string]string{"type": roomsdomain.FrameListRooms}, roomsdomain.FrameRooms)
	if err != nil {
		return nil, err
	}
	var listed struct {
		Rooms []roomsdomain.RoomStatus `json:"rooms"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil, err
	}
	return listed.Rooms, nil
}

// Join follows room, whose members welcome the user on their first visit.
func (r *Rooms) Join(ctx context.Context, room string) (roomsdomain.Snapshot, error) {
	if err := r.thread.dialLazily(ctx); err != nil {
		return roomsdomain.Snapshot{}, err
	}
	return r.thread.followRoom(ctx, roomsdomain.FrameJoinRoom, room)
}

// Post posts text to room and returns where it went.
func (r *Rooms) Post(ctx context.Context, room, text string) (roomsdomain.Posted, error) {
	var posted roomsdomain.Posted
	if err := r.thread.dialLazily(ctx); err != nil {
		return posted, err
	}
	data, err := r.thread.Request(ctx, map[string]string{"type": roomsdomain.FramePostRoom, "room": room, "text": text}, roomsdomain.FrameRoomPosted)
	if err != nil {
		return posted, err
	}
	err = json.Unmarshal(data, &posted)
	return posted, err
}

// Decide answers member's tool call toolCallID waiting in room. The daemon
// refuses a call that was already decided.
func (r *Rooms) Decide(ctx context.Context, room, member, toolCallID string, decision roomsdomain.Decision) error {
	if err := r.thread.dialLazily(ctx); err != nil {
		return err
	}
	_, err := r.thread.Request(ctx, map[string]string{
		"type": roomsdomain.FrameDecideRoomApproval, "room": room, "member": member,
		"tool_call_id": toolCallID, "decision": string(decision),
	}, roomsdomain.FrameRoomApprovalDecided)
	return err
}

// Leave stops following room.
func (r *Rooms) Leave(room string) error {
	r.thread.unfollowRoom(room)
	return r.thread.Send(map[string]string{"type": roomsdomain.FrameLeaveRoom, "room": room})
}

// followRoom sends a join_room or follow_room frame for room and returns its
// snapshot, remembering the room so a reconnect follows it again.
func (t *Thread) followRoom(ctx context.Context, frameType, room string) (roomsdomain.Snapshot, error) {
	data, err := t.Request(ctx, map[string]string{"type": frameType, "room": room}, roomsdomain.FrameRoomSnapshot)
	if err != nil {
		return roomsdomain.Snapshot{}, err
	}
	var snapshot roomsdomain.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return roomsdomain.Snapshot{}, err
	}
	t.mu.Lock()
	t.room = room
	t.mu.Unlock()
	return snapshot, nil
}

// unfollowRoom forgets room, so a reconnect no longer follows it.
func (t *Thread) unfollowRoom(room string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.room == room {
		t.room = ""
	}
}

// refollowRoom follows the room the thread followed before its connection
// dropped, without a welcome, and hands the UI the room as it is now. A plain
// chat, which was told the daemon was lost, is told the room is back.
func (t *Thread) refollowRoom(ctx context.Context) {
	t.mu.Lock()
	room := t.room
	t.mu.Unlock()
	if room == "" {
		return
	}
	snapshot, err := t.followRoom(ctx, roomsdomain.FrameFollowRoom, room)
	if err != nil {
		logger.Debug("daemon chat could not follow its room again", "room", room, "error", err)
		return
	}
	t.post(tui.RoomSnapshotEvent{Snapshot: snapshot})
	if t.sessionless {
		t.post(tui.SetStatusEvent{Message: "#" + room + " is back", StatusType: tui.StatusDefault})
	}
}

// onRoomPost hands the UI a new post of a followed room.
func (t *Thread) onRoomPost(frame []byte) {
	var f struct {
		Room string           `json:"room"`
		Post roomsdomain.Post `json:"post"`
	}
	if json.Unmarshal(frame, &f) == nil {
		t.post(tui.RoomPostEvent{Room: f.Room, Post: f.Post})
	}
}

// onRoomCleared tells the UI a followed room's log was emptied.
func (t *Thread) onRoomCleared(frame []byte) {
	var f struct {
		Room string `json:"room"`
	}
	if json.Unmarshal(frame, &f) == nil {
		t.post(tui.RoomClearedEvent{Room: f.Room})
	}
}

// onRoomActivity hands the UI what a member of a followed room does now.
func (t *Thread) onRoomActivity(frame []byte) {
	var f struct {
		Room     string               `json:"room"`
		Member   string               `json:"member"`
		Activity roomsdomain.Activity `json:"activity"`
		Mode     string               `json:"mode"`
	}
	if json.Unmarshal(frame, &f) == nil {
		t.post(tui.RoomActivityEvent{Room: f.Room, Member: f.Member, Activity: f.Activity, Mode: f.Mode})
	}
}
