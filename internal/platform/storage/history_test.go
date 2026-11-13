package storage

import (
	"context"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

func TestStoresKeepTheInputHistoryDistinct(t *testing.T) {
	ctx := context.Background()
	stores, err := NewStorage(StorageConfig{Type: config.StorageTypeMemory})
	require.NoError(t, err)

	for _, command := range []string{"  ls  ", "ls", "", "   ", "ls -la", "ls", "ls"} {
		require.NoError(t, stores.ShellHistory.AppendHistory(ctx, "/work/app", command))
	}
	require.NoError(t, stores.ShellHistory.AppendHistory(ctx, "/work/other", "ls"))

	history, err := stores.ShellHistory.LoadHistory(ctx, "/work/app", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"ls", "ls -la", "ls"}, history)

	history, err = stores.ShellHistory.LoadHistory(ctx, "/work/other", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"ls"}, history)
}
