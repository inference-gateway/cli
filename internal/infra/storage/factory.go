package storage

import (
	"fmt"
)

// NewStorage creates a new storage instance based on the provided configuration
func NewStorage(config StorageConfig) (ConversationStorage, error) {
	switch config.Type {
	case "sqlite":
		return NewSQLiteStorage(config.SQLite)
	case "postgres":
		return NewPostgresStorage(config.Postgres)
	case "redis":
		return NewRedisStorage(config.Redis)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}
