package migrations

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "migration_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func TestMigrationRunner_EnsureMigrationTable(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	runner := NewMigrationRunner(db, "sqlite")
	ctx := context.Background()

	err := runner.EnsureMigrationTable(ctx)
	if err != nil {
		t.Fatalf("Failed to ensure migration table: %v", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query table existence: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected schema_migrations table to exist, got count: %d", count)
	}
}

func TestMigrationRunner_ApplyMigration(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	runner := NewMigrationRunner(db, "sqlite")
	ctx := context.Background()

	if err := runner.EnsureMigrationTable(ctx); err != nil {
		t.Fatalf("Failed to ensure migration table: %v", err)
	}

	migration := Migration{
		Version:     "001",
		Description: "Create test table",
		UpSQL: `
			CREATE TABLE test_table (
				id INTEGER PRIMARY KEY,
				name TEXT NOT NULL
			);
		`,
		DownSQL: `DROP TABLE test_table;`,
	}

	err := runner.ApplyMigration(ctx, migration)
	if err != nil {
		t.Fatalf("Failed to apply migration: %v", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='test_table'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query table existence: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected test_table to exist, got count: %d", count)
	}

	var version string
	err = db.QueryRow("SELECT version FROM schema_migrations WHERE version = ?", migration.Version).Scan(&version)
	if err != nil {
		t.Fatalf("Failed to query migration record: %v", err)
	}

	if version != migration.Version {
		t.Errorf("Expected version %s, got %s", migration.Version, version)
	}
}

func TestMigrationRunner_ApplyMigrations(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	runner := NewMigrationRunner(db, "sqlite")
	ctx := context.Background()

	migrations := []Migration{
		{
			Version:     "001",
			Description: "Create users table",
			UpSQL: `
				CREATE TABLE users (
					id INTEGER PRIMARY KEY,
					username TEXT NOT NULL
				);
			`,
		},
		{
			Version:     "002",
			Description: "Create posts table",
			UpSQL: `
				CREATE TABLE posts (
					id INTEGER PRIMARY KEY,
					user_id INTEGER NOT NULL,
					title TEXT NOT NULL
				);
			`,
		},
	}

	appliedCount, err := runner.ApplyMigrations(ctx, migrations)
	if err != nil {
		t.Fatalf("Failed to apply migrations: %v", err)
	}

	if appliedCount != 2 {
		t.Errorf("Expected 2 migrations to be applied, got %d", appliedCount)
	}

	var usersCount, postsCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&usersCount)
	_ = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='posts'").Scan(&postsCount)

	if usersCount != 1 {
		t.Errorf("Expected users table to exist")
	}
	if postsCount != 1 {
		t.Errorf("Expected posts table to exist")
	}

	appliedCount, err = runner.ApplyMigrations(ctx, migrations)
	if err != nil {
		t.Fatalf("Failed to apply migrations second time: %v", err)
	}

	if appliedCount != 0 {
		t.Errorf("Expected 0 migrations to be applied on second run, got %d", appliedCount)
	}
}

func TestMigrationRunner_GetMigrationStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	runner := NewMigrationRunner(db, "sqlite")
	ctx := context.Background()

	migrations := []Migration{
		{
			Version:     "001",
			Description: "Migration 1",
			UpSQL:       "SELECT 1;",
		},
		{
			Version:     "002",
			Description: "Migration 2",
			UpSQL:       "SELECT 1;",
		},
	}

	if err := runner.EnsureMigrationTable(ctx); err != nil {
		t.Fatalf("Failed to ensure migration table: %v", err)
	}
	if err := runner.ApplyMigration(ctx, migrations[0]); err != nil {
		t.Fatalf("Failed to apply migration: %v", err)
	}

	status, err := runner.GetMigrationStatus(ctx, migrations)
	if err != nil {
		t.Fatalf("Failed to get migration status: %v", err)
	}

	if len(status) != 2 {
		t.Fatalf("Expected 2 status entries, got %d", len(status))
	}

	if !status[0].Applied {
		t.Errorf("Expected migration 001 to be applied")
	}
	if status[0].Version != "001" {
		t.Errorf("Expected version 001, got %s", status[0].Version)
	}

	if status[1].Applied {
		t.Errorf("Expected migration 002 to not be applied")
	}
	if status[1].Version != "002" {
		t.Errorf("Expected version 002, got %s", status[1].Version)
	}
}

func TestGetSQLiteMigrations(t *testing.T) {
	migrations := GetSQLiteMigrations()

	if len(migrations) == 0 {
		t.Fatal("Expected at least one SQLite migration")
	}

	first := migrations[0]
	if first.Version == "" {
		t.Error("Expected migration to have a version")
	}
	if first.Description == "" {
		t.Error("Expected migration to have a description")
	}
	if first.UpSQL == "" {
		t.Error("Expected migration to have UpSQL")
	}
}

func TestGetPostgresMigrations(t *testing.T) {
	migrations := GetPostgresMigrations()

	if len(migrations) == 0 {
		t.Fatal("Expected at least one PostgreSQL migration")
	}

	first := migrations[0]
	if first.Version == "" {
		t.Error("Expected migration to have a version")
	}
	if first.Description == "" {
		t.Error("Expected migration to have a description")
	}
	if first.UpSQL == "" {
		t.Error("Expected migration to have UpSQL")
	}
}
