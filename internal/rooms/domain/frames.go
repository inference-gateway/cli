package domain

// The binding frames of the rooms, shared by the daemon that hosts them and
// every client of it. A client lists, joins, follows, posts to, clears and
// leaves rooms and decides a member's tool call, and a follower gets every
// post and member activity as it happens.
const (
	FrameListRooms           = "list_rooms"
	FrameRooms               = "rooms"
	FrameJoinRoom            = "join_room"
	FrameFollowRoom          = "follow_room"
	FrameRoomSnapshot        = "room_snapshot"
	FrameRoomPost            = "room_post"
	FrameRoomActivity        = "room_activity"
	FramePostRoom            = "post_room"
	FrameRoomPosted          = "room_posted"
	FrameLeaveRoom           = "leave_room"
	FrameClearRoom           = "clear_room"
	FrameRoomCleared         = "room_cleared"
	FrameDecideRoomApproval  = "decide_room_approval"
	FrameRoomApprovalDecided = "room_approval_decided"
)
