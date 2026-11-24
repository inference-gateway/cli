package domain

import "slices"

// PostKindApproval is a member's tool call waiting in its room for the user to
// decide, and PostKindDecision the room's line that closes one.
const (
	PostKindApproval PostKind = "approval"
	PostKindDecision PostKind = "decision"
)

// Approval is the tool call an approval post asks about and a decision post
// closes: the interrupt id the call waits under, its tool and arguments, and
// the decision, empty when it was decided outside the room.
type Approval struct {
	ID        string   `json:"id"`
	Tool      string   `json:"tool,omitempty"`
	Arguments string   `json:"arguments,omitempty"`
	Decision  Decision `json:"decision,omitempty"`
}

// Decision is how the user decides a member's tool call from its room.
type Decision string

// DecisionApprove runs the call, DecisionReject refuses it.
const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

// Valid reports whether d is a decision a room takes.
func (d Decision) Valid() bool {
	return d == DecisionApprove || d == DecisionReject
}

// OpenApprovals returns the approval posts of history that no decision post
// closes yet, oldest first.
func OpenApprovals(history []Post) []Post {
	var open []Post
	for _, post := range history {
		switch {
		case post.Kind == PostKindApproval && post.Approval != nil:
			open = append(open, post)
		case post.Kind == PostKindDecision && post.Approval != nil:
			open = slices.DeleteFunc(open, func(pending Post) bool { return pending.Approval.ID == post.Approval.ID })
		}
	}
	return open
}
