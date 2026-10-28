package storage

import (
	"context"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func (m *MemoryStorage) GetLease(_ context.Context, conversationID string) (agentdomain.Lease, bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	lease, ok := m.leases[conversationID]
	return lease, ok, nil
}

func (m *MemoryStorage) ClaimLease(_ context.Context, lease agentdomain.Lease) (agentdomain.Lease, bool, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if current, ok := m.leases[lease.ConversationID]; ok && !current.Yields(lease, time.Now()) {
		return current, false, nil
	}
	m.leases[lease.ConversationID] = lease
	return lease, true, nil
}

func (m *MemoryStorage) RenewLease(_ context.Context, conversationID, executor string, until time.Time) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	current, ok := m.leases[conversationID]
	if !ok || current.Executor != executor {
		return agentdomain.ErrLeaseLost
	}
	current.Until = until
	m.leases[conversationID] = current
	return nil
}

func (m *MemoryStorage) ReleaseLease(_ context.Context, conversationID, executor string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if current, ok := m.leases[conversationID]; ok && current.Executor == executor {
		delete(m.leases, conversationID)
	}
	return nil
}
