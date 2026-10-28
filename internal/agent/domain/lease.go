package domain

import (
	"context"
	"errors"
	"time"
)

// ErrLeaseLost reports a renewal or release by an executor that no longer
// holds the lease.
var ErrLeaseLost = errors.New("the session lease belongs to another executor")

// Lease records which executor owns a conversation's session: the executor
// that may run its worker, the workspace it runs in there, the transfer that
// handed it over, and when the claim lapses unless renewed.
type Lease struct {
	ConversationID string    `json:"conversation_id"`
	Executor       string    `json:"executor"`
	Workspace      string    `json:"workspace,omitempty"`
	TransferID     string    `json:"transfer_id,omitempty"`
	Until          time.Time `json:"until"`
}

// Live reports whether the lease has not lapsed at now.
func (l Lease) Live(now time.Time) bool {
	return now.Before(l.Until)
}

// Yields reports whether claim may replace this lease: the holder re-asserts
// its own row, a retry carries the same transfer id, or the lease lapsed.
func (l Lease) Yields(claim Lease, now time.Time) bool {
	if l.Executor == claim.Executor {
		return true
	}
	if claim.TransferID != "" && l.TransferID == claim.TransferID {
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
	// RenewLease moves the lapse forward while executor still holds the row,
	// and reports ErrLeaseLost otherwise.
	RenewLease(ctx context.Context, conversationID, executor string, until time.Time) error
	// ReleaseLease drops the row while executor still holds it.
	ReleaseLease(ctx context.Context, conversationID, executor string) error
}
