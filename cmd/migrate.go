package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/infra/storage"
	"github.com/inference-gateway/cli/internal/infra/storage/migrations"
	"github.com/inference-gateway/cli/internal/ui/styles/icons"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	Long: `Run database migrations to update the schema to the latest version.

This command applies any pending migrations to your database. Migrations are tracked
in the schema_migrations table to ensure they are only applied once.

The command automatically detects your database backend (SQLite, PostgreSQL, JSONL, Redis, Memory)
and applies the appropriate migrations. Note that JSONL, Redis, and Memory storage backends
do not require migrations as they do not use a relational schema.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		status, _ := cmd.Flags().GetBool("status")
		if status {
			return showMigrationStatus()
		}
		return runMigrations()
	},
}

func init() {
	migrateCmd.Flags().Bool("status", false, "Show migration status without applying migrations")
	rootCmd.AddCommand(migrateCmd)
}

// runMigrations executes pending database migrations
func runMigrations() error {
	// Get configuration from global viper
	cfg, err := getConfigFromViper()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Create service container
	serviceContainer := container.NewServiceContainer(cfg, V)

	// Get storage backend
	conversationStorage := serviceContainer.GetStorage()

	// Close storage when done
	defer func() {
		if err := conversationStorage.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close storage: %v\n", err)
		}
	}()

	// Get underlying database connection
	switch conversationStorage.(type) {
	case *storage.SQLiteStorage:
		// Migrations are automatically run during NewSQLiteStorage
		fmt.Printf("%s SQLite database migrations are up to date\n", icons.CheckMarkStyle.Render(icons.CheckMark))
		fmt.Println("   All migrations have been applied automatically")
		return nil
	case *storage.PostgresStorage:
		// Migrations are automatically run during NewPostgresStorage
		fmt.Printf("%s PostgreSQL database migrations are up to date\n", icons.CheckMarkStyle.Render(icons.CheckMark))
		fmt.Println("   All migrations have been applied automatically")
		return nil
	case *storage.JsonlStorage:
		fmt.Println("JSONL storage does not require migrations")
		return nil
	case *storage.MemoryStorage:
		fmt.Println("Memory storage does not require migrations")
		return nil
	case *storage.RedisStorage:
		fmt.Println("Redis storage does not require migrations")
		return nil
	default:
		return fmt.Errorf("unsupported storage backend: %T", conversationStorage)
	}
}

// showMigrationStatus displays the current migration status
func showMigrationStatus() error {
	// Get configuration from global viper
	cfg, err := getConfigFromViper()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Create service container
	serviceContainer := container.NewServiceContainer(cfg, V)

	// Get storage backend
	conversationStorage := serviceContainer.GetStorage()

	// Close storage when done
	defer func() {
		if err := conversationStorage.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close storage: %v\n", err)
		}
	}()

	// Check storage type and show status
	switch s := conversationStorage.(type) {
	case *storage.SQLiteStorage:
		return showSQLiteMigrationStatus(s)
	case *storage.PostgresStorage:
		return showPostgresMigrationStatus(s)
	case *storage.JsonlStorage:
		fmt.Println("JSONL storage does not require migrations")
		return nil
	case *storage.MemoryStorage:
		fmt.Println("Memory storage does not require migrations")
		return nil
	case *storage.RedisStorage:
		fmt.Println("Redis storage does not require migrations")
		return nil
	default:
		return fmt.Errorf("unsupported storage backend: %T", s)
	}
}

// showSQLiteMigrationStatus shows migration status for SQLite
func showSQLiteMigrationStatus(s *storage.SQLiteStorage) error {
	ctx := context.Background()
	db := s.DB() // We'll need to expose this
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	runner := migrations.NewMigrationRunner(db, "sqlite")
	allMigrations := migrations.GetSQLiteMigrations()

	status, err := runner.GetMigrationStatus(ctx, allMigrations)
	if err != nil {
		return fmt.Errorf("failed to get migration status: %w", err)
	}

	fmt.Println("SQLite Migration Status:")
	fmt.Println()
	for _, s := range status {
		statusIcon := "❌"
		statusText := "Pending"
		if s.Applied {
			statusIcon = "✅"
			statusText = "Applied"
		}
		fmt.Printf("  %s Version %s: %s (%s)\n", statusIcon, s.Version, s.Description, statusText)
	}

	return nil
}

// showPostgresMigrationStatus shows migration status for PostgreSQL
func showPostgresMigrationStatus(s *storage.PostgresStorage) error {
	ctx := context.Background()
	db := s.DB() // We'll need to expose this
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	runner := migrations.NewMigrationRunner(db, "postgres")
	allMigrations := migrations.GetPostgresMigrations()

	status, err := runner.GetMigrationStatus(ctx, allMigrations)
	if err != nil {
		return fmt.Errorf("failed to get migration status: %w", err)
	}

	fmt.Println("PostgreSQL Migration Status:")
	fmt.Println()
	for _, s := range status {
		statusIcon := "❌"
		statusText := "Pending"
		if s.Applied {
			statusIcon = "✅"
			statusText = "Applied"
		}
		fmt.Printf("  %s Version %s: %s (%s)\n", statusIcon, s.Version, s.Description, statusText)
	}

	return nil
}
