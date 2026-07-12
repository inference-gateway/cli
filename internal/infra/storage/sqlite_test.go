package storage

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

func setupTestStorage(t *testing.T) (*SQLiteStorage, func()) {
	tempDir, err := os.MkdirTemp("", "sqlite_test_*")
	require.NoError(t, err)

	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(SQLiteConfig{Path: dbPath})
	require.NoError(t, err)

	cleanup := func() {
		_ = storage.Close()
		_ = os.RemoveAll(tempDir)
	}

	return storage, cleanup
}

// TestSQLiteStorage_Conformance runs the shared storage suite against a real
// on-disk SQLite database (pure-Go driver, unconditional in CI).
func TestSQLiteStorage_Conformance(t *testing.T) {
	runConversationStorageConformance(t, func(t *testing.T) ConversationStorage {
		storage, cleanup := setupTestStorage(t)
		t.Cleanup(cleanup)
		return storage
	})
}
