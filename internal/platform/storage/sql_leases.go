package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// claimLeaseSQL is the compare-and-swap in one statement: insert when no row
// exists, and update only when the row yields (same holder, same non-empty
// transfer id, or lapsed). Zero affected rows means the holder kept it.
const claimLeaseSQL = `
	INSERT INTO session_leases (conversation_id, environment, workspace, transfer_id, until_unix)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(conversation_id) DO UPDATE SET
		environment = excluded.environment,
		workspace = excluded.workspace,
		transfer_id = excluded.transfer_id,
		until_unix = excluded.until_unix
	WHERE session_leases.environment = excluded.environment
	   OR (excluded.transfer_id <> '' AND session_leases.transfer_id = excluded.transfer_id)
	   OR session_leases.until_unix < ?
`

const getLeaseSQL = `SELECT environment, workspace, transfer_id, until_unix FROM session_leases WHERE conversation_id = ?`

const renewLeaseSQL = `UPDATE session_leases SET until_unix = ? WHERE conversation_id = ? AND environment = ?`

const releaseLeaseSQL = `DELETE FROM session_leases WHERE conversation_id = ? AND environment = ?`

func (s *sqlStore) GetLease(ctx context.Context, conversationID string) (agentdomain.Lease, bool, error) {
	lease := agentdomain.Lease{ConversationID: conversationID}
	var until int64
	err := s.db.QueryRowContext(ctx, s.rebind(getLeaseSQL), conversationID).
		Scan(&lease.Environment, &lease.Workspace, &lease.TransferID, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return agentdomain.Lease{}, false, nil
	}
	if err != nil {
		return agentdomain.Lease{}, false, fmt.Errorf("reading the session lease: %w", err)
	}
	lease.Until = time.Unix(until, 0).UTC()
	return lease, true, nil
}

func (s *sqlStore) ClaimLease(ctx context.Context, lease agentdomain.Lease) (agentdomain.Lease, bool, error) {
	result, err := s.db.ExecContext(ctx, s.rebind(claimLeaseSQL),
		lease.ConversationID, lease.Environment, lease.Workspace, lease.TransferID, lease.Until.Unix(), time.Now().Unix())
	if err != nil {
		return agentdomain.Lease{}, false, fmt.Errorf("claiming the session lease: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		return lease, true, nil
	}
	current, _, err := s.GetLease(ctx, lease.ConversationID)
	return current, false, err
}

func (s *sqlStore) RenewLease(ctx context.Context, conversationID, environment string, until time.Time) error {
	result, err := s.db.ExecContext(ctx, s.rebind(renewLeaseSQL), until.Unix(), conversationID, environment)
	if err != nil {
		return fmt.Errorf("renewing the session lease: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return agentdomain.ErrLeaseLost
	}
	return nil
}

func (s *sqlStore) ReleaseLease(ctx context.Context, conversationID, environment string) error {
	if _, err := s.db.ExecContext(ctx, s.rebind(releaseLeaseSQL), conversationID, environment); err != nil {
		return fmt.Errorf("releasing the session lease: %w", err)
	}
	return nil
}
