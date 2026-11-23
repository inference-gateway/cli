package domain

import (
	"testing"

	require "github.com/stretchr/testify/require"
)

func TestUnseen(t *testing.T) {
	history := []Post{
		{ID: "p0", Author: UserAuthor},
		{ID: "p1", Author: "tester"},
		{ID: "p2", Author: UserAuthor},
		{ID: "p3", Author: "writer"},
		{ID: "p4", Author: UserAuthor},
	}
	tests := []struct {
		name      string
		member    string
		seen      int
		addressed string
		limit     int
		want      []string
	}{
		{"a first turn starts after the member's last post", "tester", UnknownSeen, "p4", 10, []string{"p2", "p3"}},
		{"a later turn starts at the first post it has not seen", "tester", 3, "p4", 10, []string{"p3"}},
		{"posts after the addressed one come along too", "tester", 2, "p2", 10, []string{"p3", "p4"}},
		{"the member's own posts are left out", "tester", 0, "p4", 10, []string{"p0", "p2", "p3"}},
		{"only the last limit posts come along", "tester", 0, "p4", 2, []string{"p2", "p3"}},
		{"nothing unseen", "tester", 5, "p4", 10, []string{}},
		{"a member that never posted sees everything", "planner", UnknownSeen, "p2", 10, []string{"p0", "p1", "p3", "p4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := []string{}
			for _, post := range Unseen(history, tt.member, tt.seen, tt.addressed, tt.limit) {
				ids = append(ids, post.ID)
			}
			require.Equal(t, tt.want, ids)
		})
	}
}
