package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	migrations "github.com/inference-gateway/cli/internal/infra/storage/migrations"
	_ "modernc.org/sqlite"
)

// SQLiteStorage implements ConversationStorage on top of the shared sqlStore
// core, backed by a local pure-Go SQLite file.
type SQLiteStorage struct {
	*sqlStore
}

// NewSQLiteStorage creates a new SQLite storage instance.
func NewSQLiteStorage(config SQLiteConfig) (*SQLiteStorage, error) {
	if err := verifySQLiteAvailable(); err != nil {
		return nil, err
	}

	dir := filepath.Dir(config.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", config.Path+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=1000&_timeout=30000&_busy_timeout=30000")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	runner := migrations.NewMigrationRunner(db, "sqlite")
	if _, err := runner.ApplyMigrations(context.Background(), migrations.GetSQLiteMigrations()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &SQLiteStorage{&sqlStore{db: db, dialect: "sqlite"}}, nil
}

// verifySQLiteAvailable checks if SQLite is available (using pure Go implementation)
func verifySQLiteAvailable() error {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return fmt.Errorf("SQLite driver not available (pure Go implementation): %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("SQLite connection test failed: %w", err)
	}

	return nil
}
