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
	return listed.Rooms, json.Unmarshal(data, &listed)
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
	return posted, json.Unmarshal(data, &posted)
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
// dropped, without a welcome, and hands the UI the room as it is now.
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
	}
	if json.Unmarshal(frame, &f) == nil {
		t.post(tui.RoomActivityEvent{Room: f.Room, Member: f.Member, Activity: f.Activity})
	}
}
