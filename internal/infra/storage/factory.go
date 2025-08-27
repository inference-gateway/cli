package storage

import (
	"fmt"
	"path/filepath"

	"github.com/inference-gateway/cli/config"
)

// NewStorageFromConfig creates a storage configuration from app config
func NewStorageFromConfig(cfg *config.Config) StorageConfig {
	if !cfg.Storage.Enabled {
		return StorageConfig{Type: "memory"}
	}

	storageType := cfg.Storage.Type

	switch storageType {
	case "sqlite":
		path := cfg.Storage.SQLite.Path
		if !filepath.IsAbs(path) {
			absPath, err := filepath.Abs(path)
			if err == nil {
				path = absPath
			}
		}
		return StorageConfig{
			Type: "sqlite",
			SQLite: SQLiteConfig{
				Path: path,
			},
		}
	case "postgres":
		return StorageConfig{
			Type: "postgres",
			Postgres: PostgresConfig{
				Host:     cfg.Storage.Postgres.Host,
				Port:     cfg.Storage.Postgres.Port,
				Database: cfg.Storage.Postgres.Database,
				Username: cfg.Storage.Postgres.Username,
				Password: cfg.Storage.Postgres.Password,
				SSLMode:  cfg.Storage.Postgres.SSLMode,
			},
		}
	case "redis":
		return StorageConfig{
			Type: "redis",
			Redis: RedisConfig{
				Host:     cfg.Storage.Redis.Host,
				Port:     cfg.Storage.Redis.Port,
				Password: cfg.Storage.Redis.Password,
				Database: cfg.Storage.Redis.DB,
			},
		}
	case "memory":
		return StorageConfig{
			Type: "memory",
		}
	default:
		return StorageConfig{
			Type: "memory",
		}
	}
}

// NewStorage creates a new storage instance based on the provided configuration
func NewStorage(config StorageConfig) (ConversationStorage, error) {
	switch config.Type {
	case "sqlite":
		return NewSQLiteStorage(config.SQLite)
	case "postgres":
		return NewPostgresStorage(config.Postgres)
	case "redis":
		return NewRedisStorage(config.Redis)
	case "memory":
		return NewMemoryStorage(), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}
