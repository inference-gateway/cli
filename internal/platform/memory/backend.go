package memory

import (
	"context"
)

// MemoryBackend syncs the persistent memory directory with a remote. The local
// backend is a no-op; the git backend pulls on run start and commits + pushes
// when a fact changes. Both directions are best-effort: an error is returned for
// tests/telemetry but callers log and continue - a sync failure never aborts the
// agent run. SyncIn is idempotent and runs at most once per process.
type MemoryBackend interface {
	SyncIn(ctx context.Context) error
	SyncOut(ctx context.Context) error
}
