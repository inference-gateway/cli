package rooms

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	config "github.com/inference-gateway/cli/config"
	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// The binding frames of the rooms. A client lists, follows, posts to and
// leaves rooms, and a follower gets every post and member activity as it
// happens.
const (
	frameListRooms    = "list_rooms"
	frameRooms        = "rooms"
	frameFollowRoom   = "follow_room"
	frameRoomSnapshot = "room_snapshot"
	frameRoomPost     = "room_post"
	frameRoomActivity = "room_activity"
	framePostRoom     = "post_room"
	frameRoomPosted   = "room_posted"
	frameLeaveRoom    = "leave_room"
	frameJoinRoom     = "join_room"
	frameRunError     = "RUN_ERROR"
)

// routeTimeout bounds the decision request that routes one post.
const routeTimeout = time.Minute

// Host is the daemon's rooms. It routes and logs the posts clients write,
// delivers each routed post to its member as the member's next turn on the
// thread registry, logs the member's answer, and streams every room to its
// followers. One lock orders the log's appends with the follows, so a
// follower sees each post exactly once.
type Host struct {
	ctx     context.Context
	cfg     config.RoomsConfig
	threads sessionsdomain.ThreadRouter
	log     roomsdomain.Log
	router  *Router
	agents  AgentCatalog

	mu        sync.Mutex
	members   map[string]*member
	followers map[string][]sessionsdomain.Client
}

// NewHost returns the rooms of cfg. It delivers to members through threads,
// logs to log, and asks decider about the posts that ping nobody.
func NewHost(ctx context.Context, cfg config.RoomsConfig, threads sessionsdomain.ThreadRouter, log roomsdomain.Log, decider decisiondomain.Decider, agents AgentCatalog) *Host {
	return &Host{
		ctx:       ctx,
		cfg:       cfg,
		threads:   threads,
		log:       log,
		router:    NewRouter(decider, cfg),
		agents:    agents,
		members:   map[string]*member{},
		followers: map[string][]sessionsdomain.Client{},
	}
}

// clientFrame is one rooms frame a client sends.
type clientFrame struct {
	Type string `json:"type"`
	Room string `json:"room"`
	Text string `json:"text"`
}

type roomsFrame struct {
	Type  string                   `json:"type"`
	Rooms []roomsdomain.RoomStatus `json:"rooms"`
}

type snapshotFrame struct {
	Type string `json:"type"`
	roomsdomain.Snapshot
}

type postFrame struct {
	Type string           `json:"type"`
	Room string           `json:"room"`
	Post roomsdomain.Post `json:"post"`
}

type activityFrame struct {
	Type     string               `json:"type"`
	Room     string               `json:"room"`
	Member   string               `json:"member"`
	Activity roomsdomain.Activity `json:"activity"`
}

// postedFrame answers post_room to its poster.
type postedFrame struct {
	Type string `json:"type"`
	roomsdomain.Posted
}

// Handle claims the rooms frames of one client and reports false for any
// other. Listing, posting and welcomes run off the binding's read loop, while
// follow, join and leave stay on it so a quick leave never overtakes its follow.
func (h *Host) Handle(c sessionsdomain.Client, frame []byte) bool {
	var f clientFrame
	if json.Unmarshal(frame, &f) != nil {
		return false
	}
	switch f.Type {
	case frameListRooms:
		go h.listRooms(c)
	case frameFollowRoom:
		h.follow(c, f.Room)
	case framePostRoom:
		go h.post(c, f.Room, f.Text)
	case frameJoinRoom:
		h.join(c, f.Room)
	case frameLeaveRoom:
		h.leave(c, f.Room)
	default:
		return false
	}
	return true
}

// Detach drops a client that disconnected from every room it followed.
func (h *Host) Detach(c sessionsdomain.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for name := range h.followers {
		h.unfollowLocked(c, name)
	}
}

// Close stops every member from following its thread when the daemon stops.
func (h *Host) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.members {
		m.stop()
	}
}

func (h *Host) listRooms(c sessionsdomain.Client) {
	resolved := Resolve(h.cfg, h.agents)
	views := make([]roomsdomain.RoomStatus, 0, len(resolved))
	h.mu.Lock()
	for _, room := range resolved {
		views = append(views, h.viewLocked(room))
	}
	h.mu.Unlock()
	c.Deliver(encode(roomsFrame{Type: frameRooms, Rooms: views}))
}

// follow answers follow_room with the room's snapshot and streams the room to
// c from then on.
func (h *Host) follow(c sessionsdomain.Client, name string) {
	room, ok := h.findRoom(name)
	if !ok {
		c.Deliver(unknownRoom(name))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.followLocked(c, room)
}

// join answers join_room as follow does and, on the user's first visit to the
// room, logs their join for the first max_members members to welcome.
func (h *Host) join(c sessionsdomain.Client, name string) {
	room, ok := h.findRoom(name)
	if !ok {
		c.Deliver(unknownRoom(name))
		return
	}
	if joined, firstVisit := h.followFirstVisit(c, room); firstVisit {
		go h.deliverTurns(room, joined)
	}
}

// followFirstVisit follows room for c and logs the user's join when they were
// never in it, under the one lock, so two joins never welcome twice.
func (h *Host) followFirstVisit(c sessionsdomain.Client, room roomsdomain.Room) (roomsdomain.Post, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	posts, ok := h.followLocked(c, room)
	if !ok || !roomsdomain.FirstVisit(posts) {
		return roomsdomain.Post{}, false
	}
	joined := h.joinPost(room)
	if err := h.appendLocked(room, joined); err != nil {
		logger.Warn("rooms could not log a join", "room", room.Name, "error", err)
		return roomsdomain.Post{}, false
	}
	return joined, true
}

// followLocked answers c with the room's snapshot and streams the room to it
// from then on. It returns the posts so far.
func (h *Host) followLocked(c sessionsdomain.Client, room roomsdomain.Room) ([]roomsdomain.Post, bool) {
	posts, err := h.log.Read(room)
	if err != nil {
		c.Deliver(runError(fmt.Sprintf("reading room %s: %v", room.Name, err)))
		return nil, false
	}
	if !slices.Contains(h.followers[room.Name], c) {
		h.followers[room.Name] = append(h.followers[room.Name], c)
	}
	c.Deliver(encode(snapshotFrame{Type: frameRoomSnapshot, Room: h.viewLocked(room), Posts: posts}))
	return posts, true
}

// joinPost is the user's join of room, routed to the first max_members
// members, who welcome them.
func (h *Host) joinPost(room roomsdomain.Room) roomsdomain.Post {
	post := newPost(roomsdomain.UserAuthor, "joined #"+room.Name)
	post.Kind = roomsdomain.PostKindJoin
	welcomers := room.Agents[:min(len(room.Agents), cmp.Or(h.cfg.Router.MaxMembers, config.DefaultRoomsMaxMembers))]
	for _, member := range welcomers {
		post.Routes = append(post.Routes, roomsdomain.Route{Member: member, Probability: mentionProbability})
	}
	return post
}

func (h *Host) leave(c sessionsdomain.Client, name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unfollowLocked(c, name)
}

func (h *Host) unfollowLocked(c sessionsdomain.Client, name string) {
	h.followers[name] = slices.DeleteFunc(h.followers[name], func(f sessionsdomain.Client) bool { return f == c })
	if len(h.followers[name]) == 0 {
		delete(h.followers, name)
	}
}

// post routes the user's post, logs it with its routes, answers the poster
// with the notices, and hands it to every member it was routed to. A failed
// decision request still logs the post, routed to nobody.
func (h *Host) post(c sessionsdomain.Client, name, text string) {
	if strings.TrimSpace(text) == "" {
		c.Deliver(runError("a room post needs text"))
		return
	}
	room, ok := h.findRoom(name)
	if !ok {
		c.Deliver(unknownRoom(name))
		return
	}
	history, err := h.log.Read(room)
	if err != nil {
		c.Deliver(runError(fmt.Sprintf("reading room %s: %v", name, err)))
		return
	}
	post := newPost(roomsdomain.UserAuthor, text)
	routing, err := h.route(room, history, post)
	if err != nil {
		logger.Warn("rooms could not route a post", "room", name, "error", err)
		routing.Notices = append(routing.Notices, fmt.Sprintf("The post reached nobody, routing failed: %v. Mention a member to reach one.", err))
	}
	post.Mentions, post.Routes = routing.Mentions, routing.Routes
	if err := h.append(room, post); err != nil {
		c.Deliver(runError(fmt.Sprintf("logging the post in room %s: %v", name, err)))
		return
	}
	c.Deliver(encode(postedFrame{Type: frameRoomPosted, Room: name, Post: post, Notices: routing.Notices}))
	h.deliverTurns(room, post)
}

// deliverTurns hands post to every member it was routed to.
func (h *Host) deliverTurns(room roomsdomain.Room, post roomsdomain.Post) {
	for _, route := range post.Routes {
		h.member(room, route.Member).enqueue(post)
	}
}

func (h *Host) route(room roomsdomain.Room, history []roomsdomain.Post, post roomsdomain.Post) (Routing, error) {
	ctx, cancel := context.WithTimeout(h.ctx, routeTimeout)
	defer cancel()
	return h.router.Route(ctx, room, h.agents(room.Project), history, post)
}

// append logs post in room and streams it to the room's followers.
func (h *Host) append(room roomsdomain.Room, post roomsdomain.Post) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.appendLocked(room, post)
}

func (h *Host) appendLocked(room roomsdomain.Room, post roomsdomain.Post) error {
	if err := h.log.Append(room, post); err != nil {
		return err
	}
	h.deliverLocked(room.Name, encode(postFrame{Type: frameRoomPost, Room: room.Name, Post: post}))
	return nil
}

// note logs what the room has to say about a member, such as one waiting on
// an approval in its own chat.
func (h *Host) note(room roomsdomain.Room, text string) {
	if err := h.append(room, newPost(roomsdomain.RoomAuthor, text)); err != nil {
		logger.Warn("rooms could not log a note", "room", room.Name, "error", err)
	}
}

// announce streams a member's new activity to the room's followers.
func (h *Host) announce(room roomsdomain.Room, agent string, activity roomsdomain.Activity) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.deliverLocked(room.Name, encode(activityFrame{Type: frameRoomActivity, Room: room.Name, Member: agent, Activity: activity}))
}

func (h *Host) deliverLocked(name string, frame []byte) {
	for _, follower := range h.followers[name] {
		follower.Deliver(frame)
	}
}

// member returns the seat of agent in room, starting its frame handling on
// first use.
func (h *Host) member(room roomsdomain.Room, agent string) *member {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := room.SessionID(agent)
	if m, ok := h.members[id]; ok {
		return m
	}
	m := newMember(h, room, agent)
	h.members[id] = m
	go m.run()
	return m
}

func (h *Host) findRoom(name string) (roomsdomain.Room, bool) {
	return Find(Resolve(h.cfg, h.agents), name)
}

// viewLocked renders room with what each of its members is doing.
func (h *Host) viewLocked(room roomsdomain.Room) roomsdomain.RoomStatus {
	members := make([]roomsdomain.MemberStatus, 0, len(room.Agents))
	for _, seat := range room.Members() {
		activity := roomsdomain.ActivityIdle
		if m, ok := h.members[seat.SessionID]; ok {
			activity = m.currentActivity()
		}
		members = append(members, roomsdomain.MemberStatus{Agent: seat.Agent, SessionID: seat.SessionID, Activity: activity})
	}
	return roomsdomain.RoomStatus{Name: room.Name, Project: room.Project, Members: members}
}

// newPost stamps a new post with the id a member's turn finds it by in the
// log, since two posts can share a time.
func newPost(author, text string) roomsdomain.Post {
	return roomsdomain.Post{ID: uuid.NewString(), Author: author, Text: text, Time: time.Now()}
}

func unknownRoom(name string) []byte {
	return runError(fmt.Sprintf("room %q not found", name))
}

func runError(message string) []byte {
	return encode(map[string]string{"type": frameRunError, "message": message})
}

// encode marshals one frame of plain data, which cannot fail.
func encode(frame any) []byte {
	data, _ := json.Marshal(frame)
	return data
}
