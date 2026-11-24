package domain

import (
	"testing"

	require "github.com/stretchr/testify/require"
)

func approvalPost(id string) Post {
	return Post{ID: "post-" + id, Author: "tester", Kind: PostKindApproval, Approval: &Approval{ID: id, Tool: "Bash"}}
}

func decisionPost(id string) Post {
	return Post{Author: RoomAuthor, Kind: PostKindDecision, Approval: &Approval{ID: id, Decision: DecisionApprove}}
}

func TestOpenApprovals(t *testing.T) {
	tests := []struct {
		name    string
		history []Post
		want    []string
	}{
		{"none in an empty log", nil, nil},
		{"an approval nobody decided is open", []Post{approvalPost("a")}, []string{"a"}},
		{"a decision closes its approval only", []Post{approvalPost("a"), approvalPost("b"), decisionPost("a")}, []string{"b"}},
		{"a message is never an approval", []Post{{Author: "tester", Text: "hi"}}, nil},
		{"an approval without its call is not decidable", []Post{{Author: "tester", Kind: PostKindApproval}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, post := range OpenApprovals(tt.history) {
				got = append(got, post.Approval.ID)
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDecisionValid(t *testing.T) {
	require.True(t, DecisionApprove.Valid())
	require.True(t, DecisionReject.Valid())
	require.False(t, Decision("auto").Valid(), "a room offers approve and reject only")
	require.False(t, Decision("").Valid())
}
