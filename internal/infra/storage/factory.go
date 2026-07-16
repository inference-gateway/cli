package storage

import (
	"fmt"
	"path/filepath"

	config "github.com/inference-gateway/cli/config"
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
	case "d1":
		return StorageConfig{
			Type: "d1",
			D1: D1Config{
				AccountID:  cfg.Storage.D1.AccountID,
				DatabaseID: cfg.Storage.D1.DatabaseID,
				APIToken:   cfg.Storage.D1.APIToken,
				BaseURL:    cfg.Storage.D1.BaseURL,
			},
		}
	case "jsonl":
		path := cfg.Storage.Jsonl.Path
		if !filepath.IsAbs(path) {
			absPath, err := filepath.Abs(path)
			if err == nil {
				path = absPath
			}
		}
		return StorageConfig{
			Type: "jsonl",
			Jsonl: JsonlStorageConfig{
				Path: path,
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
func NewStorage(config StorageConfig) (*Stores, error) {
	switch config.Type {
	case "sqlite":
		s, err := NewSQLiteStorage(config.SQLite)
		if err != nil {
			return nil, err
		}
		return &Stores{
			Conversations: s,
			SessionGroups: s,
			ScheduledJobs: s,
			Plans:         s,
			ShellHistory:  s,
		}, nil
	case "postgres":
		s, err := NewPostgresStorage(config.Postgres)
		if err != nil {
			return nil, err
		}
		return &Stores{
			Conversations: s,
			SessionGroups: s,
			ScheduledJobs: s,
			Plans:         s,
			ShellHistory:  s,
		}, nil
	case "redis":
		s, err := NewRedisStorage(config.Redis)
		if err != nil {
			return nil, err
		}
		return &Stores{
			Conversations: s,
			SessionGroups: s,
			ScheduledJobs: s,
			Plans:         s,
			ShellHistory:  s,
		}, nil
	case "d1":
		s, err := NewD1Storage(config.D1)
		if err != nil {
			return nil, err
		}
		return &Stores{
			Conversations: s,
			SessionGroups: s,
			ScheduledJobs: s,
			Plans:         s,
			ShellHistory:  s,
		}, nil
	case "jsonl":
		s, err := NewJsonlStorage(config.Jsonl)
		if err != nil {
			return nil, err
		}
		return &Stores{
			Conversations: s,
			SessionGroups: s,
			ScheduledJobs: s,
			Plans:         s,
			ShellHistory:  s,
		}, nil
	case "memory":
		s := NewMemoryStorage()
		return &Stores{
			Conversations: s,
			SessionGroups: s,
			ScheduledJobs: s,
			Plans:         s,
			ShellHistory:  s,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}
