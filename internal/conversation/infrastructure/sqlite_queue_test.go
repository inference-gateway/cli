package infrastructure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// newSQLiteQueueAt builds a durable sqlite queue over path under prefix. The
// scope is unique per subtest but shared by every adapter the subtest builds,
// so the restart and drain-race groups see one backend while groups stay
// isolated from each other.
func newSQLiteQueueAt(t *testing.T, prefix, path string) convdomain.MessageQueue {
	t.Helper()
	return newScopedTestQueue(t, prefix, func(scope string) (convdomain.MessageQueue, error) {
		return NewSQLiteMessageQueue(storage.SQLiteConfig{Path: path}, scope, time.Hour, func() {})
	})
}

func TestSQLiteMessageQueue_Conformance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.db")
	prefix := fmt.Sprintf("infer-test-%d", time.Now().UnixNano())
	newQueue := func(t *testing.T) convdomain.MessageQueue {
		return newSQLiteQueueAt(t, prefix, path)
	}

	runMessageQueueConformance(t, newQueue)
	runMessageQueueRestartPersistence(t, newQueue)
	runMessageQueueDrainRace(t, newQueue)
}

func TestSQLiteMessageQueue_ScopesByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.db")
	ctx := context.Background()

	owner, err := NewSQLiteMessageQueue(storage.SQLiteConfig{Path: path}, "q-one", time.Hour, func() {})
	require.NoError(t, err)
	t.Cleanup(func() { owner.(*durableMessageQueue).Stop() })
	require.NoError(t, owner.Enqueue(ctx, userMsg("a"), convdomain.QueueSourceComposer, "r1"))

	other, err := NewSQLiteMessageQueue(storage.SQLiteConfig{Path: path}, "q-two", time.Hour, func() {})
	require.NoError(t, err)
	t.Cleanup(func() { other.(*durableMessageQueue).Stop() })

	drained, err := other.Drain(ctx)
	require.NoError(t, err)
	assert.Empty(t, drained)
}

func TestSQLiteMessageQueue_UnusableDatabaseFailsStartup(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("occupies the directory name"), 0o600))

	_, err := NewSQLiteMessageQueue(storage.SQLiteConfig{Path: filepath.Join(blocker, "queue.db")}, "main", time.Hour, func() {})
	require.Error(t, err)
	assert.ErrorContains(t, err, "queue backend sqlite")
}
