package storage

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestStorageFactory(t *testing.T) {
	t.Run("SQLite Storage", func(t *testing.T) {
		config := StorageConfig{
			Type: "sqlite",
			SQLite: SQLiteConfig{
				Path: ":memory:",
			},
		}

		stores, err := NewStorage(config)
		require.NoError(t, err)
		assert.IsType(t, &SQLiteStorage{}, stores.Conversations)

		err = stores.Conversations.Close()
		assert.NoError(t, err)
	})

	t.Run("Redis Storage - Invalid Config", func(t *testing.T) {
		config := StorageConfig{
			Type: "redis",
			Redis: RedisConfig{
				Host: "invalid-host",
				Port: 6379,
			},
		}

		_, err := NewStorage(config)
		assert.Error(t, err)
	})

	t.Run("Postgres Storage - Invalid Config", func(t *testing.T) {
		config := StorageConfig{
			Type: "postgres",
			Postgres: PostgresConfig{
				Host:     "invalid-host",
				Port:     5432,
				Database: "testdb",
				Username: "test",
				Password: "test",
				SSLMode:  "disable",
			},
		}

		_, err := NewStorage(config)
		assert.Error(t, err)
	})

	t.Run("JSONL Storage", func(t *testing.T) {
		tempDir := t.TempDir()

		config := StorageConfig{
			Type: "jsonl",
			Jsonl: JsonlStorageConfig{
				Path: tempDir,
			},
		}

		stores, err := NewStorage(config)
		require.NoError(t, err)
		assert.IsType(t, &JsonlStorage{}, stores.Conversations)

		err = stores.Conversations.Close()
		assert.NoError(t, err)
	})

	t.Run("Memory Storage", func(t *testing.T) {
		config := StorageConfig{
			Type: "memory",
		}

		stores, err := NewStorage(config)
		require.NoError(t, err)
		assert.IsType(t, &MemoryStorage{}, stores.Conversations)

		err = stores.Conversations.Close()
		assert.NoError(t, err)
	})

	t.Run("Unsupported Storage Type", func(t *testing.T) {
		config := StorageConfig{
			Type: "unsupported",
		}

		_, err := NewStorage(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported storage type")
	})
}
