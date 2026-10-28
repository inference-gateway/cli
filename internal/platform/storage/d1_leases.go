package storage

import (
	"context"
	"fmt"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func (s *D1Storage) GetLease(ctx context.Context, conversationID string) (agentdomain.Lease, bool, error) {
	rows, err := s.queryRows(ctx, getLeaseSQL, conversationID)
	if err != nil {
		return agentdomain.Lease{}, false, fmt.Errorf("reading the session lease: %w", err)
	}
	if len(rows) == 0 {
		return agentdomain.Lease{}, false, nil
	}
	row := rows[0]
	return agentdomain.Lease{
		ConversationID: conversationID,
		Executor:       asString(row["executor"]),
		Workspace:      asString(row["workspace"]),
		TransferID:     asString(row["transfer_id"]),
		Until:          time.Unix(int64(asInt(row["until_unix"])), 0).UTC(),
	}, true, nil
}

func (s *D1Storage) ClaimLease(ctx context.Context, lease agentdomain.Lease) (agentdomain.Lease, bool, error) {
	changes, err := s.exec(ctx, claimLeaseSQL,
		lease.ConversationID, lease.Executor, lease.Workspace, lease.TransferID, lease.Until.Unix(), time.Now().Unix())
	if err != nil {
		return agentdomain.Lease{}, false, fmt.Errorf("claiming the session lease: %w", err)
	}
	if changes > 0 {
		return lease, true, nil
	}
	current, _, err := s.GetLease(ctx, lease.ConversationID)
	return current, false, err
}

func (s *D1Storage) RenewLease(ctx context.Context, conversationID, executor string, until time.Time) error {
	changes, err := s.exec(ctx, renewLeaseSQL, until.Unix(), conversationID, executor)
	if err != nil {
		return fmt.Errorf("renewing the session lease: %w", err)
	}
	if changes == 0 {
		return agentdomain.ErrLeaseLost
	}
	return nil
}

func (s *D1Storage) ReleaseLease(ctx context.Context, conversationID, executor string) error {
	if _, err := s.exec(ctx, releaseLeaseSQL, conversationID, executor); err != nil {
		return fmt.Errorf("releasing the session lease: %w", err)
	}
	return nil
}
