package utils

import (
	"encoding/hex"
	"testing"
)

func TestRandomToken(t *testing.T) {
	first, err := RandomToken()
	if err != nil {
		t.Fatalf("RandomToken() error = %v, want nil", err)
	}
	if len(first) != 48 {
		t.Errorf("RandomToken() length = %d, want 48", len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Errorf("RandomToken() = %q, not valid hex: %v", first, err)
	}

	if second, err := RandomToken(); err != nil {
		t.Fatalf("RandomToken() error = %v, want nil", err)
	} else if second == first {
		t.Errorf("RandomToken() returned the same token twice: %q", first)
	}
}
