package memory

import (
	"context"
	"testing"
)

func TestLocalBackend_NoOp(t *testing.T) {
	b := NewLocalBackend()
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn: want nil, got %v", err)
	}
	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut: want nil, got %v", err)
	}
}
