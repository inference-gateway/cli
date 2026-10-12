package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	migrations "github.com/inference-gateway/cli/internal/platform/storage/migrations"
)

// PostgresStorage implements ConversationStorage on top of the shared sqlStore
// core. It speaks the same single-table schema as SQLite/D1; only
// the placeholder style differs, which sqlStore.rebind normalizes.
type PostgresStorage struct {
	*sqlStore
}

// verifyPostgresAvailable checks if PostgreSQL is available
func verifyPostgresAvailable(config PostgresConfig) error {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.Username, config.Password, config.Database, config.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("PostgreSQL driver not available: %w\n\n"+
			"PostgreSQL connection failed. Verify:\n"+
			"  - PostgreSQL server is running\n"+
			"  - Connection details are correct\n"+
			"  - Network connectivity to %s:%d", err, config.Host, config.Port)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("PostgreSQL connection test failed: %w\n\n"+
			"Failed to connect to PostgreSQL. Verify:\n"+
			"  - PostgreSQL server is running at %s:%d\n"+
			"  - Database '%s' exists\n"+
			"  - User '%s' has proper permissions\n"+
			"  - Network connectivity is working", err, config.Host, config.Port, config.Database, config.Username)
	}

	return nil
}

// NewPostgresStorage creates a new PostgreSQL storage instance
func NewPostgresStorage(config PostgresConfig) (*PostgresStorage, error) {
	db, err := OpenPostgres(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runner := migrations.NewMigrationRunner(db, "postgres")
	if _, err := runner.ApplyMigrations(ctx, migrations.GetPostgresMigrations()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &PostgresStorage{&sqlStore{db: db, dialect: "postgres"}}, nil
}
