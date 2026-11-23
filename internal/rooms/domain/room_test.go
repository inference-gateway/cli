package domain

import (
	"testing"

	require "github.com/stretchr/testify/require"
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
// the agents that exist at resolution time, so a newly added agent joins the
// room without a rooms.yaml edit, names stand for themselves and duplicates
// collapse.
func TestResolveMemberAgents(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		known    []string
		want     []string
	}{
		{
			name:     "wildcard resolves to every known agent, sorted",
			patterns: []string{AgentsWildcard},
			known:    []string{"zeta", "main", "explore"},
			want:     []string{"explore", "main", "zeta"},
		},
		{
			name:     "a newly added agent joins on the next resolution",
			patterns: []string{AgentsWildcard},
			known:    []string{"main", "writer"},
			want:     []string{"main", "writer"},
		},
		{
			name:     "explicit names stand for themselves",
			patterns: []string{"researcher", "tester"},
			known:    []string{"researcher", "tester", "writer"},
			want:     []string{"researcher", "tester"},
		},
		{
			name:     "a name no agent carries takes no seat",
			patterns: []string{"reviwer", "tester"},
			known:    []string{"reviewer", "tester"},
			want:     []string{"tester"},
		},
		{
			name:     "duplicates collapse keeping their first position",
			patterns: []string{"tester", "tester"},
			known:    []string{"tester"},
			want:     []string{"tester"},
		},
		{
			name:     "an explicit name under the wildcard is not repeated",
			patterns: []string{AgentsWildcard, "main"},
			known:    []string{"main", "explore"},
			want:     []string{"explore", "main"},
		},
		{
			name:     "no patterns resolve to nobody",
			patterns: nil,
			known:    []string{"main"},
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResolveMemberAgents(tt.patterns, tt.known))
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
