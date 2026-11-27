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

// frameRunError is the AG-UI error frame the rooms answer a failed request with.
const frameRunError = "RUN_ERROR"

// routeTimeout bounds the decision request that routes one post.
const routeTimeout = time.Minute

// Host is the daemon's rooms. It routes and logs the posts clients write,
// delivers each routed post to its member as the member's next turn on the
// thread registry, logs the member's answer, and streams every room to its
// followers. settings reads rooms.yaml as it is now, so an edit reaches the
// next post. One lock orders the log's appends with the follows.
type Host struct {
	ctx      context.Context
	settings func() config.RoomsConfig
	threads  sessionsdomain.ThreadRouter
	log      roomsdomain.Log
	decider  decisiondomain.Decider
	agents   AgentCatalog
	place    Placer

	mu        sync.Mutex
	members   map[string]*member
	followers map[string][]sessionsdomain.Client
}

// NewHost returns the rooms that settings reads each time they are used. It
// delivers to members through threads, logs to log, asks decider about the
// posts that ping nobody, and moves a member whose agent pins a workspace there
// with place, which nil leaves on the daemon's machine.
func NewHost(ctx context.Context, settings func() config.RoomsConfig, threads sessionsdomain.ThreadRouter, log roomsdomain.Log, decider decisiondomain.Decider, agents AgentCatalog, place Placer) *Host {
	return &Host{
		ctx:       ctx,
		settings:  settings,
		threads:   threads,
		log:       log,
		decider:   decider,
		agents:    agents,
		place:     place,
		members:   map[string]*member{},
		followers: map[string][]sessionsdomain.Client{},
	}
}

// clientFrame is one rooms frame a client sends. Member, ToolCallID and
// Decision belong to decide_room_approval.
type clientFrame struct {
	Type       string               `json:"type"`
	Room       string               `json:"room"`
	Text       string               `json:"text"`
	Member     string               `json:"member"`
	ToolCallID string               `json:"tool_call_id"`
	Decision   roomsdomain.Decision `json:"decision"`
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

type roomFrame struct {
	Type string `json:"type"`
	Room string `json:"room"`
}

// activityFrame streams what a member does now, and the agent mode its turn
// runs in, so a client shows the mode the room's members actually run in.
type activityFrame struct {
	Type     string               `json:"type"`
	Room     string               `json:"room"`
	Member   string               `json:"member"`
	Activity roomsdomain.Activity `json:"activity"`
	Mode     string               `json:"mode,omitempty"`
}

// postedFrame answers post_room to its poster.
type postedFrame struct {
	Type string `json:"type"`
	roomsdomain.Posted
}

// stoppedFrame answers stop_room with the members that were stopped, none when
// nobody was at work.
type stoppedFrame struct {
	Type    string   `json:"type"`
	Room    string   `json:"room"`
	Members []string `json:"members"`
}

// decidedFrame answers decide_room_approval to the client that decided.
type decidedFrame struct {
	Type       string               `json:"type"`
	Room       string               `json:"room"`
	Member     string               `json:"member"`
	ToolCallID string               `json:"tool_call_id"`
	Decision   roomsdomain.Decision `json:"decision"`
}

// Handle claims the rooms frames of one client and reports false for any
// other. Listing, posting, clearing, deciding and welcomes run off the binding's
// read loop, while follow, join and leave stay on it so a quick leave never
// overtakes its follow.
func (h *Host) Handle(c sessionsdomain.Client, frame []byte) bool {
	var f clientFrame
	if json.Unmarshal(frame, &f) != nil {
		return false
	}
	switch f.Type {
	case roomsdomain.FrameListRooms:
		go h.listRooms(c)
	case roomsdomain.FrameFollowRoom:
		h.follow(c, f.Room)
	case roomsdomain.FramePostRoom:
		go h.post(c, f.Room, f.Text)
	case roomsdomain.FrameClearRoom:
		go h.clear(c, f.Room)
	case roomsdomain.FrameDecideRoomApproval:
		go h.decide(c, f)
	case roomsdomain.FrameStopRoom:
		go h.stop(c, f)
	case roomsdomain.FrameJoinRoom:
		h.join(c, f.Room)
	case roomsdomain.FrameLeaveRoom:
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
	resolved := Resolve(h.settings(), h.agents)
	views := make([]roomsdomain.RoomStatus, 0, len(resolved))
	h.mu.Lock()
	for _, room := range resolved {
		views = append(views, h.viewLocked(room))
	}
	h.mu.Unlock()
	c.Deliver(encode(roomsFrame{Type: roomsdomain.FrameRooms, Rooms: views}))
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
	c.Deliver(encode(snapshotFrame{Type: roomsdomain.FrameRoomSnapshot, Room: h.viewLocked(room), Posts: posts}))
	return posts, true
}

// joinPost is the user's join of room, routed to the first max_members
// members, who welcome them.
func (h *Host) joinPost(room roomsdomain.Room) roomsdomain.Post {
	post := newPost(roomsdomain.UserAuthor, "joined #"+room.Name)
	post.Kind = roomsdomain.PostKindJoin
	welcomers := room.Agents[:min(len(room.Agents), h.settings().Router.EffectiveMaxMembers())]
	for _, member := range welcomers {
		post.Routes = append(post.Routes, roomsdomain.Route{Member: member, Probability: mentionProbability})
	}
	return post
}

// clear empties room's log for everyone: its followers and c get room_cleared,
// and each member drops the posts it held and reads the room from its start.
func (h *Host) clear(c sessionsdomain.Client, name string) {
	room, ok := h.findRoom(name)
	if !ok {
		c.Deliver(unknownRoom(name))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.log.Clear(room); err != nil {
		c.Deliver(runError(fmt.Sprintf("clearing room %s: %v", name, err)))
		return
	}
	for _, m := range h.members {
		if m.room.Name == name {
			m.startOver()
		}
	}
	cleared := encode(roomFrame{Type: roomsdomain.FrameRoomCleared, Room: name})
	h.deliverLocked(name, cleared)
	if !slices.Contains(h.followers[name], c) {
		c.Deliver(cleared)
	}
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
	post.Mentions, post.Routes, post.Usage = routing.Mentions, routing.Routes, roomsdomain.Usage{Tokens: routing.Tokens}
	if err := h.append(room, post); err != nil {
		c.Deliver(runError(fmt.Sprintf("logging the post in room %s: %v", name, err)))
		return
	}
	c.Deliver(encode(postedFrame{Type: roomsdomain.FrameRoomPosted, Room: name, Post: post, Notices: routing.Notices}))
	h.deliverTurns(room, post)
}

// deliverTurns hands post to every member it was routed to, and an offered
// post to the first of them only, who takes it or passes it on.
func (h *Host) deliverTurns(room roomsdomain.Room, post roomsdomain.Post) {
	routes := post.Routes
	if post.Offered() {
		routes = routes[:min(len(routes), 1)]
	}
	for _, route := range routes {
		h.member(room, route.Member).enqueue(post)
	}
}

// handOver logs a member's answer and hands it to the members it pings as their
// next turn, unless the chain of hand-overs between members reached max_hops,
// which the room notes. A pinged outsider gets a note too.
func (h *Host) handOver(room roomsdomain.Room, post roomsdomain.Post) {
	settings := h.settings()
	routing := NewRouter(h.decider, settings).RouteHandover(room, h.agents(room.Project), post)
	if maxHops := settings.Router.EffectiveMaxHops(); len(routing.Routes) > 0 && post.Hop > maxHops {
		routing.Notices = append(routing.Notices, fmt.Sprintf("%s pinged %s, but the post stops here: members handed it on %d times, the most rooms.router.max_hops allows.", post.Author, pinged(routing.Routes), maxHops))
		routing.Routes = nil
	}
	post.Mentions, post.Routes = routing.Mentions, routing.Routes
	if err := h.append(room, post); err != nil {
		logger.Warn("rooms could not log a member's answer", "room", room.Name, "member", post.Author, "error", err)
		return
	}
	for _, notice := range routing.Notices {
		h.note(room, notice)
	}
	h.deliverTurns(room, post)
}

// pinged names the members of routes as @mentions, in one list.
func pinged(routes []roomsdomain.Route) string {
	names := make([]string, 0, len(routes))
	for _, route := range routes {
		names = append(names, "@"+route.Member)
	}
	return strings.Join(names, ", ")
}

// passOn offers post to the member after the one that passed it on, or tells
// the room that nobody picked it up. The note carries what the pass spent.
func (h *Host) passOn(room roomsdomain.Room, post roomsdomain.Post, passed string, spent roomsdomain.Usage) {
	next, ok := post.OfferedAfter(passed)
	if !ok {
		h.noteSpent(room, fmt.Sprintf("%s passed it on and nobody picked the post up. Mention a member to hand it to one.", passed), spent)
		return
	}
	h.noteSpent(room, fmt.Sprintf("%s passed it on - offered to %s.", passed, next), spent)
	h.member(room, next).enqueue(post)
}

// decide answers decide_room_approval: the member resumes its run with the
// decision, the room logs who decided what, and c gets room_approval_decided.
// A call that is no longer open, decided or never asked, gets a refusal.
func (h *Host) decide(c sessionsdomain.Client, f clientFrame) {
	if !f.Decision.Valid() {
		c.Deliver(runError(fmt.Sprintf("decision %q: must be %s or %s", f.Decision, roomsdomain.DecisionApprove, roomsdomain.DecisionReject)))
		return
	}
	room, ok := h.findRoom(f.Room)
	if !ok {
		c.Deliver(unknownRoom(f.Room))
		return
	}
	h.mu.Lock()
	m := h.members[room.SessionID(f.Member)]
	h.mu.Unlock()
	if m == nil {
		c.Deliver(runError(fmt.Sprintf("%s's call was already decided", f.Member)))
		return
	}
	decided, err := m.decide(f.ToolCallID, f.Decision)
	if err != nil {
		c.Deliver(runError(err.Error()))
		return
	}
	h.closeApproval(room, decided, fmt.Sprintf("You %s %s's %s call.", decisionVerb(f.Decision), f.Member, toolLabel(decided)))
	c.Deliver(encode(decidedFrame{Type: roomsdomain.FrameRoomApprovalDecided, Room: room.Name, Member: f.Member, ToolCallID: f.ToolCallID, Decision: f.Decision}))
}

// stop answers stop_room: every member at work in the room, or the one f names,
// aborts its turn and drops the posts it held, the room notes each, and c gets
// room_stopped with who was stopped. A member outside the room is refused.
func (h *Host) stop(c sessionsdomain.Client, f clientFrame) {
	room, ok := h.findRoom(f.Room)
	if !ok {
		c.Deliver(unknownRoom(f.Room))
		return
	}
	if f.Member != "" && !slices.Contains(room.Agents, f.Member) {
		c.Deliver(runError(fmt.Sprintf("%s is not in room %s", f.Member, room.Name)))
		return
	}
	stopped := []string{}
	for _, m := range h.seatsOf(room, f.Member) {
		if m.abort() {
			stopped = append(stopped, m.agent)
			h.note(room, m.agent+" was stopped.")
		}
	}
	c.Deliver(encode(stoppedFrame{Type: roomsdomain.FrameRoomStopped, Room: room.Name, Members: stopped}))
}

// seatsOf are the seats of room this daemon started, in the room's order, or
// agent's seat alone when agent is named.
func (h *Host) seatsOf(room roomsdomain.Room, agent string) []*member {
	h.mu.Lock()
	defer h.mu.Unlock()
	var seats []*member
	for _, seat := range room.Members() {
		if m, ok := h.members[seat.SessionID]; ok && (agent == "" || seat.Agent == agent) {
			seats = append(seats, m)
		}
	}
	return seats
}

// askApproval posts a member's tool call that waits for the user, with note,
// the judge's reason and the member's justification when it escalated.
func (h *Host) askApproval(room roomsdomain.Room, agent string, approval roomsdomain.Approval, note string) {
	post := newPost(agent, note)
	post.Kind, post.Approval = roomsdomain.PostKindApproval, &approval
	if err := h.append(room, post); err != nil {
		logger.Warn("rooms could not post an approval", "room", room.Name, "member", agent, "error", err)
	}
}

// closeApproval logs the room's line that closes an approval post, saying who
// decided the call and how.
func (h *Host) closeApproval(room roomsdomain.Room, approval roomsdomain.Approval, text string) {
	post := newPost(roomsdomain.RoomAuthor, text)
	post.Kind, post.Approval = roomsdomain.PostKindDecision, &approval
	if err := h.append(room, post); err != nil {
		logger.Warn("rooms could not close an approval", "room", room.Name, "error", err)
	}
}

func decisionVerb(decision roomsdomain.Decision) string {
	if decision == roomsdomain.DecisionApprove {
		return "approved"
	}
	return "rejected"
}

// toolLabel names the tool of a call, or says "tool" for a call without one.
func toolLabel(approval roomsdomain.Approval) string {
	return cmp.Or(approval.Tool, "tool")
}

func (h *Host) route(room roomsdomain.Room, history []roomsdomain.Post, post roomsdomain.Post) (Routing, error) {
	ctx, cancel := context.WithTimeout(h.ctx, routeTimeout)
	defer cancel()
	return NewRouter(h.decider, h.settings()).Route(ctx, room, h.agents(room.Project), history, post)
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
	h.deliverLocked(room.Name, encode(postFrame{Type: roomsdomain.FrameRoomPost, Room: room.Name, Post: post}))
	return nil
}

// note logs what the room has to say about a member, such as one waiting on
// an approval in its own chat.
func (h *Host) note(room roomsdomain.Room, text string) {
	h.noteSpent(room, text, roomsdomain.Usage{})
}

// noteSpent logs a note about a member's turn with what the turn spent, such
// as an offered post passed on.
func (h *Host) noteSpent(room roomsdomain.Room, text string, spent roomsdomain.Usage) {
	post := newPost(roomsdomain.RoomAuthor, text)
	post.Usage = spent
	if err := h.append(room, post); err != nil {
		logger.Warn("rooms could not log a note", "room", room.Name, "error", err)
	}
}

// announce streams a member's new activity to the room's followers, with the
// mode room runs its members in.
func (h *Host) announce(room roomsdomain.Room, status roomsdomain.MemberStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.deliverLocked(room.Name, encode(activityFrame{
		Type: roomsdomain.FrameRoomActivity, Room: room.Name, Member: status.Agent, Activity: status.Activity, Mode: room.Mode,
	}))
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
	return h.roomIn(h.settings(), name)
}

// roomIn resolves the room called name as settings and the agents of its
// project have it now.
func (h *Host) roomIn(settings config.RoomsConfig, name string) (roomsdomain.Room, bool) {
	return Find(Resolve(settings, h.agents), name)
}

// viewLocked renders room with what each of its members is doing.
func (h *Host) viewLocked(room roomsdomain.Room) roomsdomain.RoomStatus {
	roles := roomsdomain.Roles(h.agents(room.Project))
	members := make([]roomsdomain.MemberStatus, 0, len(room.Agents))
	for _, seat := range room.Members() {
		status := roomsdomain.MemberStatus{Agent: seat.Agent, Role: roles[seat.Agent], SessionID: seat.SessionID, Activity: roomsdomain.ActivityIdle}
		if m, ok := h.members[seat.SessionID]; ok {
			status.Activity = m.currentActivity()
		}
		members = append(members, status)
	}
	return roomsdomain.RoomStatus{Name: room.Name, Project: room.Project, Mode: room.Mode, Members: members}
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
