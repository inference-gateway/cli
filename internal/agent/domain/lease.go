package domain

import (
	"context"
	"errors"
	"time"
)

// ErrLeaseLost reports a renewal or release by a workspace that no longer
// holds the lease.
var ErrLeaseLost = errors.New("the session lease belongs to another workspace")

// Lease records which workspace owns a conversation's session: the workspace
// that may run its worker, the workspace dir it runs in there, the teleport that
// handed it over, and when the claim lapses unless renewed.
type Lease struct {
	ConversationID string    `json:"conversation_id"`
	Workspace      string    `json:"workspace"`
	WorkspaceDir   string    `json:"workspace_dir,omitempty"`
	TeleportID     string    `json:"teleport_id,omitempty"`
	Until          time.Time `json:"until"`
}

// Live reports whether the lease has not lapsed at now.
func (l Lease) Live(now time.Time) bool {
	return now.Before(l.Until)
}

// Yields reports whether claim may replace this lease: the holder re-asserts
// its own row, a retry carries the same teleport id, or the lease lapsed.
func (l Lease) Yields(claim Lease, now time.Time) bool {
	if l.Workspace == claim.Workspace {
		return true
	}
	if claim.TeleportID != "" && l.TeleportID == claim.TeleportID {
		return true
	}
	return !l.Live(now)
}

// LeaseStore keeps the session leases next to the conversations, so every
// daemon sharing a storage backend sees who owns a session.
type LeaseStore interface {
	// GetLease returns the lease of a conversation, false when none is recorded.
	GetLease(ctx context.Context, conversationID string) (Lease, bool, error)
	// ClaimLease writes lease when no row exists or the current row yields to
	// it, and otherwise returns the current holder and false.
	ClaimLease(ctx context.Context, lease Lease) (Lease, bool, error)
	// RenewLease moves the lapse forward while workspace still holds the row,
	// and reports ErrLeaseLost otherwise.
	RenewLease(ctx context.Context, conversationID, workspace string, until time.Time) error
	// ReleaseLease drops the row while workspace still holds it.
	ReleaseLease(ctx context.Context, conversationID, workspace string) error
}
