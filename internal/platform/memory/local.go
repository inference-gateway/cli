package memory

import "context"

// LocalBackend is the default no-op memory backend. Memory lives only on the
// local disk, so there is nothing to sync in or out.
type LocalBackend struct{}

// NewLocalBackend returns the no-op backend.
func NewLocalBackend() *LocalBackend { return &LocalBackend{} }

// SyncIn is a no-op.
func (LocalBackend) SyncIn(context.Context) error { return nil }

// SyncOut is a no-op.
func (LocalBackend) SyncOut(context.Context) error { return nil }
