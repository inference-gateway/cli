package storage

import (
	"context"
	"fmt"

	_ "modernc.org/sqlite"

	migrations "github.com/inference-gateway/cli/internal/platform/storage/migrations"
)

// SQLiteStorage implements ConversationStorage on top of the shared sqlStore
// core, backed by a local pure-Go SQLite file.
type SQLiteStorage struct {
	*sqlStore
}

// NewSQLiteStorage creates a new SQLite storage instance.
func NewSQLiteStorage(config SQLiteConfig) (*SQLiteStorage, error) {
	db, err := OpenSQLite(config.Path)
	if err != nil {
		return nil, err
	}

	runner := migrations.NewMigrationRunner(db, "sqlite")
	if _, err := runner.ApplyMigrations(context.Background(), migrations.GetSQLiteMigrations()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &SQLiteStorage{&sqlStore{db: db, dialect: "sqlite"}}, nil
}
