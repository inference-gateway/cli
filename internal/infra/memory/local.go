// Package memory provides pluggable sync backends for the agent's persistent
// memory directory (the fact-files + MEMORY.md index shipped in #670). The
// backend is selected by memory.backend.type: "local" (default) is a no-op;
// "git" clones/pulls the directory on run start and commits/pushes it back when
// a fact changes. The factory here mirrors internal/infra/storage/factory.go.
package memory

import "context"

// LocalBackend is the default no-op memory backend. Memory lives only on the
// local disk, so there is nothing to sync in or out - this preserves the
// pre-#683 behavior exactly.
type LocalBackend struct{}

// NewLocalBackend returns the no-op backend.
func NewLocalBackend() *LocalBackend { return &LocalBackend{} }

// SyncIn is a no-op.
func (LocalBackend) SyncIn(context.Context) error { return nil }

// SyncOut is a no-op.
func (LocalBackend) SyncOut(context.Context) error { return nil }
