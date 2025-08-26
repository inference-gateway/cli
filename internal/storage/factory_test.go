package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorageFactory(t *testing.T) {
	t.Run("SQLite Storage", func(t *testing.T) {
		config := StorageConfig{
			Type: "sqlite",
			SQLite: SQLiteConfig{
				Path: ":memory:",
			},
		}

		storage, err := NewStorage(config)
		require.NoError(t, err)
		assert.IsType(t, &SQLiteStorage{}, storage)

		err = storage.Close()
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

		// This should fail since we can't connect to invalid-host
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

		// This should fail since we can't connect to invalid-host
		_, err := NewStorage(config)
		assert.Error(t, err)
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
