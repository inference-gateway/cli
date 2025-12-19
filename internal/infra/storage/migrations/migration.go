package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Migration represents a database migration
type Migration struct {
	// Version is the migration version (e.g., "001", "002")
	Version string
	// Description is a human-readable description of the migration
	Description string
	// UpSQL contains the SQL statements to apply the migration
	UpSQL string
	// DownSQL contains the SQL statements to rollback the migration (optional)
	DownSQL string
}

// MigrationRunner manages database migrations
type MigrationRunner struct {
	db      *sql.DB
	dialect string // "sqlite" or "postgres"
}

// NewMigrationRunner creates a new migration runner
func NewMigrationRunner(db *sql.DB, dialect string) *MigrationRunner {
	return &MigrationRunner{
		db:      db,
		dialect: dialect,
	}
}

// EnsureMigrationTable creates the migration tracking table if it doesn't exist
func (r *MigrationRunner) EnsureMigrationTable(ctx context.Context) error {
	var createSQL string

	switch r.dialect {
	case "sqlite":
		createSQL = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_schema_migrations_version ON schema_migrations(version);
		`
	case "postgres":
		createSQL = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_schema_migrations_version ON schema_migrations(version);
		`
	default:
		return fmt.Errorf("unsupported dialect: %s", r.dialect)
	}

	_, err := r.db.ExecContext(ctx, createSQL)
	if err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	return nil
}

// GetAppliedMigrations returns a list of applied migration versions
func (r *MigrationRunner) GetAppliedMigrations(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("failed to scan migration version: %w", err)
		}
		applied[version] = true
	}

	return applied, rows.Err()
}

// ApplyMigration applies a single migration
func (r *MigrationRunner) ApplyMigration(ctx context.Context, migration Migration) error {
	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Execute migration SQL
	if _, err := tx.ExecContext(ctx, migration.UpSQL); err != nil {
		return fmt.Errorf("failed to execute migration %s: %w", migration.Version, err)
	}

	// Record migration as applied
	var recordSQL string
	switch r.dialect {
	case "sqlite":
		recordSQL = "INSERT INTO schema_migrations (version, description, applied_at) VALUES (?, ?, ?)"
	case "postgres":
		recordSQL = "INSERT INTO schema_migrations (version, description, applied_at) VALUES ($1, $2, $3)"
	}

	if _, err := tx.ExecContext(ctx, recordSQL, migration.Version, migration.Description, time.Now()); err != nil {
		return fmt.Errorf("failed to record migration %s: %w", migration.Version, err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration %s: %w", migration.Version, err)
	}

	return nil
}

// ApplyMigrations applies all pending migrations
func (r *MigrationRunner) ApplyMigrations(ctx context.Context, migrations []Migration) (int, error) {
	// Ensure migration table exists
	if err := r.EnsureMigrationTable(ctx); err != nil {
		return 0, err
	}

	// Get applied migrations
	applied, err := r.GetAppliedMigrations(ctx)
	if err != nil {
		return 0, err
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	// Apply pending migrations
	appliedCount := 0
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue // Skip already applied migrations
		}

		if err := r.ApplyMigration(ctx, migration); err != nil {
			return appliedCount, fmt.Errorf("migration %s failed: %w", migration.Version, err)
		}

		appliedCount++
	}

	return appliedCount, nil
}

// GetMigrationStatus returns the current migration status
func (r *MigrationRunner) GetMigrationStatus(ctx context.Context, availableMigrations []Migration) ([]MigrationStatus, error) {
	// Ensure migration table exists
	if err := r.EnsureMigrationTable(ctx); err != nil {
		return nil, err
	}

	// Get applied migrations
	applied, err := r.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	// Build status list
	var status []MigrationStatus
	for _, migration := range availableMigrations {
		status = append(status, MigrationStatus{
			Version:     migration.Version,
			Description: migration.Description,
			Applied:     applied[migration.Version],
		})
	}

	// Sort by version
	sort.Slice(status, func(i, j int) bool {
		return status[i].Version < status[j].Version
	})

	return status, nil
}

// MigrationStatus represents the status of a migration
type MigrationStatus struct {
	Version     string
	Description string
	Applied     bool
}
