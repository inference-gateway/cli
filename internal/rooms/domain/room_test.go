package domain

import (
	"testing"

	require "github.com/stretchr/testify/require"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestRoomSessionID(t *testing.T) {
	tests := []struct {
		name    string
		room    string
		agent   string
		session string
	}{
		{"public main", "public", "main", "room-public-main"},
		{"release writer", "release", "code-writer", "room-release-code-writer"},
		{"underscores survive", "my_room", "agent_1", "room-my_room-agent_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			room := Room{Name: tt.room}
			require.Equal(t, tt.session, room.SessionID(tt.agent))
		})
	}
}

// TestRoomMembers pins the member seats: one Member per resolved agent, each
// carrying the session id it keeps for the room.
func TestRoomMembers(t *testing.T) {
	room := Room{Name: "public", Project: "/repo", Agents: []string{"main", "explore"}}
	require.Equal(t, []Member{
		{Agent: "main", SessionID: "room-public-main"},
		{Agent: "explore", SessionID: "room-public-explore"},
	}, room.Members())
}

// TestResolveMemberAgents covers the room's agent entries: the wildcard follows
// the user's own agents at resolution time, so a newly added agent joins the
// room without a rooms.yaml edit, the built-ins join only by name, names stand
// for themselves and duplicates collapse.
func TestResolveMemberAgents(t *testing.T) {
	own := func(names ...string) []agentdomain.SubagentInfo {
		agents := make([]agentdomain.SubagentInfo, 0, len(names))
		for _, name := range names {
			agents = append(agents, agentdomain.SubagentInfo{Name: name})
		}
		return agents
	}
	builtIn := func(names ...string) []agentdomain.SubagentInfo {
		agents := own(names...)
		for i := range agents {
			agents[i].BuiltIn = true
		}
		return agents
	}
	tests := []struct {
		name     string
		patterns []string
		agents   []agentdomain.SubagentInfo
		want     []string
	}{
		{
			name:     "wildcard resolves to every agent of the user's own, sorted",
			patterns: []string{AgentsWildcard},
			agents:   own("zeta", "writer", "tester"),
			want:     []string{"tester", "writer", "zeta"},
		},
		{
			name:     "wildcard leaves the built-ins out",
			patterns: []string{AgentsWildcard},
			agents:   append(own("writer"), builtIn("main", "explore")...),
			want:     []string{"writer"},
		},
		{
			name:     "a built-in joins by name",
			patterns: []string{AgentsWildcard, "main"},
			agents:   append(own("writer"), builtIn("main", "explore")...),
			want:     []string{"writer", "main"},
		},
		{
			name:     "explicit names stand for themselves",
			patterns: []string{"researcher", "tester"},
			agents:   own("researcher", "tester", "writer"),
			want:     []string{"researcher", "tester"},
		},
		{
			name:     "a name no agent carries takes no seat",
			patterns: []string{"reviwer", "tester"},
			agents:   own("reviewer", "tester"),
			want:     []string{"tester"},
		},
		{
			name:     "duplicates collapse keeping their first position",
			patterns: []string{"tester", AgentsWildcard, "tester"},
			agents:   own("tester", "writer"),
			want:     []string{"tester", "writer"},
		},
		{
			name:     "no patterns resolve to nobody",
			patterns: nil,
			agents:   own("writer"),
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResolveMemberAgents(tt.patterns, tt.agents))
		})
	}
}

func TestValidName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"public", true},
		{"release_1", true},
		{"My_room_2", true},
		{"dev-ops", false},
		{"9room", true},
		{"", false},
		{"../evil", false},
		{"room/1", false},
		{".hidden", false},
		{"has space", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.valid, ValidName(tt.name))
		})
	}
}

func TestPostedRouteSummary(t *testing.T) {
	tests := []struct {
		name   string
		posted Posted
		want   string
	}{
		{
			name: "pinged and routed members",
			posted: Posted{Post: Post{
				Mentions: []string{"tester"},
				Routes:   []Route{{Member: "tester", Probability: 1}, {Member: "writer", Probability: 0.8}},
			}},
			want: "Routed to tester (pinged), writer (0.80).",
		},
		{
			name: "@all pings every member",
			posted: Posted{Post: Post{
				Mentions: []string{EveryoneMention},
				Routes:   []Route{{Member: "tester", Probability: 1}, {Member: "writer", Probability: 1}},
			}},
			want: "Routed to tester (pinged), writer (pinged).",
		},
		{
			name: "an offered post goes to its members in turn",
			posted: Posted{Post: Post{
				Routes: []Route{{Member: "researcher", Probability: 0.94}, {Member: "tester", Probability: 0.81}},
			}},
			want: "Offered to researcher (0.94), then tester (0.81).",
		},
		{name: "routed to nobody", want: "Routed to nobody."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.posted.RouteSummary())
		})
	}
}

func TestSpent(t *testing.T) {
	tests := []struct {
		name    string
		members []MemberStatus
		posts   []Post
		want    Usage
	}{
		{"a room that spent nothing", []MemberStatus{{Agent: "tester"}}, []Post{{Author: UserAuthor}}, Usage{}},
		{
			"every member's session and every routed post",
			[]MemberStatus{{Agent: "tester", Usage: Usage{Tokens: 1200, Cost: 0.01}}, {Agent: "writer", Usage: Usage{Tokens: 800, Cost: 0.02}}},
			[]Post{{Author: UserAuthor, RouterTokens: 300}, {Author: "tester"}, {Author: UserAuthor, RouterTokens: 250}},
			Usage{Tokens: 2550, Cost: 0.03},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Spent(tt.members, tt.posts)
			require.Equal(t, tt.want.Tokens, got.Tokens)
			require.InDelta(t, tt.want.Cost, got.Cost, 1e-9)
		})
	}
}

func TestPostOffered(t *testing.T) {
	tests := []struct {
		name string
		post Post
		want bool
	}{
		{"a message that pings nobody", Post{Author: UserAuthor, Text: "what's the weather?"}, true},
		{"a ping", Post{Author: UserAuthor, Mentions: []string{"tester"}}, false},
		{"an @all", Post{Author: UserAuthor, Mentions: []string{EveryoneMention}}, false},
		{"a join", Post{Author: UserAuthor, Kind: PostKindJoin}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.post.Offered())
		})
	}
}

func TestPostOfferedAfter(t *testing.T) {
	post := Post{Routes: []Route{{Member: "researcher"}, {Member: "tester"}}}
	next, ok := post.OfferedAfter("researcher")
	require.True(t, ok)
	require.Equal(t, "tester", next)
	_, ok = post.OfferedAfter("tester")
	require.False(t, ok, "the last member passes it on to nobody")
	_, ok = post.OfferedAfter("writer")
	require.False(t, ok, "a member the post was never offered to")
}

func TestRoomPasses(t *testing.T) {
	offering := Room{Name: "public", Pickup: PickupOffer}
	offered := Post{Author: UserAuthor, Text: "what's the weather?"}
	tests := []struct {
		name      string
		room      Room
		addressed Post
		answer    string
		want      bool
	}{
		{"PASS on an offered post", offering, offered, "PASS", true},
		{"PASS in any case, with a full stop", offering, offered, " pass.\n", true},
		{"an answer that mentions PASS", offering, offered, "PASS - ask researcher", false},
		{"a room that hands posts to its top match", Room{Pickup: PickupTop}, offered, "PASS", false},
		{"a pinged post cannot be passed on", offering, Post{Author: UserAuthor, Mentions: []string{"tester"}}, "PASS", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.room.Passes(tt.addressed, tt.answer))
		})
	}
}
