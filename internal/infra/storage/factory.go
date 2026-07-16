package storage

import (
	"fmt"
	"path/filepath"

	config "github.com/inference-gateway/cli/config"
)

// NewStorageFromConfig creates a storage configuration from app config
func NewStorageFromConfig(cfg *config.Config) StorageConfig {
	if !cfg.Storage.Enabled {
		return StorageConfig{Type: config.StorageTypeMemory}
	}

	switch cfg.Storage.Type {
	case config.StorageTypeSQLite:
		return StorageConfig{
			Type: config.StorageTypeSQLite,
			SQLite: SQLiteConfig{
				Path: absPath(cfg.Storage.SQLite.Path),
			},
		}
	case config.StorageTypePostgres:
		return StorageConfig{
			Type: config.StorageTypePostgres,
			Postgres: PostgresConfig{
				Host:     cfg.Storage.Postgres.Host,
				Port:     cfg.Storage.Postgres.Port,
				Database: cfg.Storage.Postgres.Database,
				Username: cfg.Storage.Postgres.Username,
				Password: cfg.Storage.Postgres.Password,
				SSLMode:  cfg.Storage.Postgres.SSLMode,
			},
		}
	case config.StorageTypeRedis:
		return StorageConfig{
			Type: config.StorageTypeRedis,
			Redis: RedisConfig{
				Host:     cfg.Storage.Redis.Host,
				Port:     cfg.Storage.Redis.Port,
				Password: cfg.Storage.Redis.Password,
				Database: cfg.Storage.Redis.DB,
			},
		}
	case config.StorageTypeD1:
		return StorageConfig{
			Type: config.StorageTypeD1,
			D1: D1Config{
				AccountID:  cfg.Storage.D1.AccountID,
				DatabaseID: cfg.Storage.D1.DatabaseID,
				APIToken:   cfg.Storage.D1.APIToken,
				BaseURL:    cfg.Storage.D1.BaseURL,
			},
		}
	case config.StorageTypeJsonl:
		return StorageConfig{
			Type: config.StorageTypeJsonl,
			Jsonl: JsonlStorageConfig{
				Path: absPath(cfg.Storage.Jsonl.Path),
			},
		}
	default:
		return StorageConfig{Type: config.StorageTypeMemory}
	}
}

// absPath resolves a relative storage path against the working directory,
// falling back to the input when resolution fails.
func absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// fullBackend is the set of storage interfaces every backend implements; it
// lets NewStorage build the Stores aggregate from a single value.
type fullBackend interface {
	ConversationStorage
	SessionGroupStorage
	ScheduledJobStorage
	PlanStorage
	ShellHistoryStorage
}

// NewStorage creates a new storage instance based on the provided configuration
func NewStorage(config StorageConfig) (*Stores, error) {
	backend, err := newBackend(config)
	if err != nil {
		return nil, err
	}
	return &Stores{
		Conversations: backend,
		SessionGroups: backend,
		ScheduledJobs: backend,
		Plans:         backend,
		ShellHistory:  backend,
	}, nil
}

// newBackend constructs the configured backend.
func newBackend(cfg StorageConfig) (fullBackend, error) {
	switch cfg.Type {
	case config.StorageTypeSQLite:
		return NewSQLiteStorage(cfg.SQLite)
	case config.StorageTypePostgres:
		return NewPostgresStorage(cfg.Postgres)
	case config.StorageTypeRedis:
		return NewRedisStorage(cfg.Redis)
	case config.StorageTypeD1:
		return NewD1Storage(cfg.D1)
	case config.StorageTypeJsonl:
		return NewJsonlStorage(cfg.Jsonl)
	case config.StorageTypeMemory:
		return NewMemoryStorage(), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
