package domain

import (
	"regexp"
	"slices"
	"time"
)

// AgentsWildcard is the room.agents entry meaning "every agent that exists
// when the room is read": a newly added agent file joins the room without a
// rooms.yaml edit.
const AgentsWildcard = "*"

// InvokedByRoom is the session Origin.InvokedBy value of a room member
// session: the room started it, not a human or a delegating agent.
const InvokedByRoom = "room"

// roomNamePattern is the filesystem-safe name a room must keep: it names the
// room's log file and lands inside member session ids.
var roomNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidName reports whether name can be a room name: letters, digits, dashes
// and underscores, starting with a letter or digit.
func ValidName(name string) bool {
	return roomNamePattern.MatchString(name)
}

// Room is a named group of agents the user talks to in one project. Agents are
// the resolved members: wildcard entries already expanded by the time a Room
// is built.
type Room struct {
	Name    string
	Project string
	Agents  []string
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
	return MemberSessionID(r.Name, agent)
}

// MemberSessionID is the session id a member keeps for one room:
// room-<room>-<agent>.
func MemberSessionID(roomName, agent string) string {
	return "room-" + roomName + "-" + agent
}

// ResolveMemberAgents expands the room's agent entries into the members that
// exist: "*" becomes every known agent sorted, so the resolution follows each
// read rather than the file's moment, a name stands for itself, and duplicates
// collapse keeping their first position.
func ResolveMemberAgents(patterns, known []string) []string {
	raw := make([]string, 0, len(patterns)+len(known))
	for _, pattern := range patterns {
		if pattern == AgentsWildcard {
			raw = append(raw, slices.Sorted(slices.Values(known))...)
			continue
		}
		raw = append(raw, pattern)
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

// Post is one entry of a room's log, written by the user or a member: who
// wrote it, what it says, which agents it mentions, who it was routed to with
// the router's probability, and when.
type Post struct {
	Author   string    `json:"author"`
	Text     string    `json:"text"`
	Mentions []string  `json:"mentions,omitempty"`
	Routes   []Route   `json:"routes,omitempty"`
	Time     time.Time `json:"time"`
}

// Route records one member a post was routed to and the router's probability
// for that member.
type Route struct {
	Member      string  `json:"member"`
	Probability float64 `json:"probability"`
}
