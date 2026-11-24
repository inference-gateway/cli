package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// AgentsWildcard is the room.agents entry meaning "every agent that exists
// when the room is read": a newly added agent file joins the room without a
// rooms.yaml edit.
const AgentsWildcard = "*"

// roomNamePattern is the filesystem-safe name a room must keep: it names the
// room's log file and lands inside member session ids. It has no dash, so the
// room part of room-<room>-<agent> ends at the first one and no two seats share
// an id.
var roomNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_]*$`)

// ValidName reports whether name can be a room name: letters, digits and
// underscores, starting with a letter or digit.
func ValidName(name string) bool {
	return roomNamePattern.MatchString(name)
}

// Room is a named group of agents the user talks to in one project. Agents are
// the resolved members: wildcard entries already expanded by the time a Room
// is built. Mode is the agent mode the members' sessions run in, and Pickup
// how a post that pings nobody finds the one member who takes it.
type Room struct {
	Name    string
	Project string
	Mode    string
	Pickup  Pickup
	Agents  []string
}

// Pickup is how a room hands a post that pings nobody to one member, so no
// two members work on the same request.
type Pickup string

// PickupOffer offers the post to the best match first, who answers it or
// passes it on to the next. PickupTop hands it to the best match only.
const (
	PickupOffer Pickup = "offer"
	PickupTop   Pickup = "top"
)

// PassReply is what a member offered a post answers to pass it on.
const PassReply = "PASS"

// MayPass reports whether the member offered addressed may pass it on: an
// offered post, in a room that offers.
func (r Room) MayPass(addressed Post) bool {
	return r.Pickup == PickupOffer && addressed.Offered()
}

// Passes reports whether answer passes addressed on to the next best match:
// PASS alone, where the member may pass.
func (r Room) Passes(addressed Post, answer string) bool {
	return r.MayPass(addressed) && strings.EqualFold(strings.Trim(answer, " \t\n.!"), PassReply)
}

// Member is one agent seat in a room: the agent and the deterministic session
// id it keeps for the room.
type Member struct {
	Agent     string
	SessionID string
}

// Members returns the room's seats, one per resolved agent, in the room's
// agent order.
func (r Room) Members() []Member {
	members := make([]Member, 0, len(r.Agents))
	for _, agent := range r.Agents {
		members = append(members, Member{Agent: agent, SessionID: r.SessionID(agent)})
	}
	return members
}

// SessionID is the deterministic session id one member agent keeps in this
// room, room-<room>-<agent>, like the channels' channel-<ch>-<sender>.
func (r Room) SessionID(agent string) string {
	return "room-" + r.Name + "-" + agent
}

// ResolveMemberAgents expands the room's agent entries into the members of
// agents: "*" becomes every agent of the user's own, sorted, the built-ins left
// out, and follows each read. A known name stands for itself, built-in or not,
// a name no agent carries takes no seat, and duplicates keep their first spot.
func ResolveMemberAgents(patterns []string, agents []agentdomain.SubagentInfo) []string {
	raw := make([]string, 0, len(patterns)+len(agents))
	for _, pattern := range patterns {
		if pattern == AgentsWildcard {
			raw = append(raw, ownAgents(agents)...)
			continue
		}
		if slices.ContainsFunc(agents, func(agent agentdomain.SubagentInfo) bool { return agent.Name == pattern }) {
			raw = append(raw, pattern)
		}
	}
	members := make([]string, 0, len(raw))
	for _, agent := range raw {
		if slices.Contains(members, agent) {
			continue
		}
		members = append(members, agent)
	}
	return members
}

// ownAgents names the agents that are not built in, sorted.
func ownAgents(agents []agentdomain.SubagentInfo) []string {
	var names []string
	for _, agent := range agents {
		if !agent.BuiltIn {
			names = append(names, agent.Name)
		}
	}
	slices.Sort(names)
	return names
}

// Post is one entry of a room's log, written by the user or a member: its id,
// who wrote it, what it says, which agents it mentions, who it was routed to
// with the router's probability, and when. Kind marks an event such as a
// join or an approval, and is empty for a message. Approval is the tool call
// an approval or decision post is about. Usage is what the post cost.
type Post struct {
	ID       string    `json:"id,omitempty"`
	Author   string    `json:"author"`
	Kind     PostKind  `json:"kind,omitempty"`
	Text     string    `json:"text"`
	Mentions []string  `json:"mentions,omitempty"`
	Routes   []Route   `json:"routes,omitempty"`
	Approval *Approval `json:"approval,omitempty"`
	Usage    Usage     `json:"usage,omitzero"`
	Time     time.Time `json:"time"`
}

// PostKind tells an event of the room's log from a message.
type PostKind string

// PostKindJoin is the post logged when the user first enters a room, which the
// members it is routed to answer with a welcome.
const PostKindJoin PostKind = "join"

// Offered reports whether post goes to its routes one member at a time, the
// best match first: a message that pings nobody, which the router ranked.
func (p Post) Offered() bool {
	return p.Kind == "" && len(p.Mentions) == 0
}

// OfferedAfter is the member post is offered to once member passed it on,
// false when member was the last of its routes.
func (p Post) OfferedAfter(member string) (string, bool) {
	i := slices.IndexFunc(p.Routes, func(route Route) bool { return route.Member == member })
	if i < 0 || i+1 >= len(p.Routes) {
		return "", false
	}
	return p.Routes[i+1].Member, true
}

// Posted is the daemon's answer to a post: the logged post with its routes,
// and the notices for its author.
type Posted struct {
	Room    string   `json:"room"`
	Post    Post     `json:"post"`
	Notices []string `json:"notices"`
}

// RouteSummary says where the post went: every member it was routed to, as
// pinged, by name or by @all, the members an offered post goes to in turn
// with the router's probability, or nobody.
func (p Posted) RouteSummary() string {
	if len(p.Post.Routes) == 0 {
		return "Routed to nobody."
	}
	routes := make([]string, 0, len(p.Post.Routes))
	for _, route := range p.Post.Routes {
		if slices.Contains(p.Post.Mentions, route.Member) || slices.Contains(p.Post.Mentions, EveryoneMention) {
			routes = append(routes, route.Member+" (pinged)")
			continue
		}
		routes = append(routes, fmt.Sprintf("%s (%.2f)", route.Member, route.Probability))
	}
	if p.Post.Offered() {
		return "Offered to " + strings.Join(routes, ", then ") + "."
	}
	return "Routed to " + strings.Join(routes, ", ") + "."
}

// RoomStatus is a room as the daemon reports it to its clients: its members,
// the session each keeps and what each is doing.
type RoomStatus struct {
	Name    string         `json:"name"`
	Project string         `json:"project"`
	Members []MemberStatus `json:"members"`
}

// MemberStatus is one member of a RoomStatus.
type MemberStatus struct {
	Agent     string   `json:"agent"`
	SessionID string   `json:"session_id"`
	Activity  Activity `json:"activity"`
}

// Usage is what a post cost: its tokens, input and output, and their cost.
type Usage struct {
	Tokens int     `json:"tokens"`
	Cost   float64 `json:"cost"`
}

// Add is u and o together.
func (u Usage) Add(o Usage) Usage {
	return Usage{Tokens: u.Tokens + o.Tokens, Cost: u.Cost + o.Cost}
}

// Spent is what a room spent since its log began: the routing of every post
// and every member turn the log records.
func Spent(posts []Post) Usage {
	var total Usage
	for _, post := range posts {
		total = total.Add(post.Usage)
	}
	return total
}

// Snapshot is what a client following a room gets first: the room and its
// log so far.
type Snapshot struct {
	Room  RoomStatus `json:"room"`
	Posts []Post     `json:"posts"`
}

// Route records one member a post was routed to and the router's probability
// for that member.
type Route struct {
	Member      string  `json:"member"`
	Probability float64 `json:"probability"`
}
