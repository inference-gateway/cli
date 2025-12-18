# Database Migrations

This document describes the database migration system in the Inference Gateway CLI.

## Overview

The CLI uses a migration system to ensure smooth upgrades between versions. Migrations are automatically applied when the
database is initialized, ensuring your schema is always up to date.

## How It Works

### Migration System

The migration system tracks which migrations have been applied using a `schema_migrations` table. Each migration has:

- **Version**: A unique identifier (e.g., "001", "002")
- **Description**: Human-readable description of what the migration does
- **UpSQL**: SQL statements to apply the migration
- **DownSQL**: SQL statements to rollback the migration (optional)

### Automatic Migrations

Migrations are **automatically applied** in the following scenarios:

1. **First time initialization**: When you run `infer init`
2. **Database connection**: When the CLI connects to the database for the first time
3. **Version upgrades**: When upgrading to a new CLI version with schema changes

### Migration Tracking

The `schema_migrations` table tracks applied migrations:

```sql
CREATE TABLE schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL
);
```

## Supported Databases

The migration system supports:

- **SQLite**: Default storage backend
- **PostgreSQL**: Production-ready relational database
- **Redis**: In-memory storage (no migrations needed)
- **Memory**: In-memory storage for testing (no migrations needed)

## Commands

### Run Migrations

Migrations are automatically run when connecting to the database. To manually trigger migrations:

```bash
# Run pending migrations
infer migrate

# Show migration status without applying
infer migrate --status
```

### Initialize with Migrations

When initializing a new project, migrations are run automatically:

```bash
# Initialize and run migrations (default)
infer init --overwrite

# Initialize without running migrations
infer init --overwrite --skip-migrations
```

### View Migration Status

Check which migrations have been applied:

```bash
infer migrate --status
```

Output example:

```text
SQLite Migration Status:

  ✅ Version 001: Initial schema - conversations table (Applied)
  ❌ Version 002: Add user preferences table (Pending)
```

## Upgrading Between Versions

### Automatic Upgrade Path

When upgrading the CLI to a newer version:

1. **Stop the CLI**: Close any running chat sessions
2. **Upgrade the binary**: Install the new version
3. **Run the CLI**: Migrations apply automatically on first use
4. **Verify**: Check status with `infer migrate --status`

### Manual Migration

If you prefer to run migrations manually:

```bash
# After upgrading, run migrations explicitly
infer migrate

# Verify all migrations applied
infer migrate --status
```

### Rollback Strategy

The migration system does not support automatic rollback. If a migration fails:

1. The transaction is rolled back automatically
2. No partial state is left in the database
3. The migration must be fixed before proceeding
4. Downgrade the CLI if needed and restore from backup

## Adding New Migrations

### For Developers

When adding schema changes:

1. **Create migration file**:
   - SQLite: `internal/infra/storage/migrations/sqlite_migrations.go`
   - PostgreSQL: `internal/infra/storage/migrations/postgres_migrations.go`

2. **Add migration to the list**:

```go
// SQLite example
{
    Version:     "002",
    Description: "Add user preferences table",
    UpSQL: `
        CREATE TABLE user_preferences (
            id TEXT PRIMARY KEY,
            user_id TEXT NOT NULL,
            settings TEXT NOT NULL
        );
        CREATE INDEX idx_user_preferences_user_id ON user_preferences(user_id);
    `,
    DownSQL: `
        DROP INDEX IF EXISTS idx_user_preferences_user_id;
        DROP TABLE IF EXISTS user_preferences;
    `,
}
```

3. **Test the migration**:

```bash
# Run tests
go test ./internal/infra/storage/migrations/...

# Test manually with a fresh database
rm ~/.infer/conversations.db
infer chat
```

4. **Document the change**: Update this file and CHANGELOG.md

### Migration Best Practices

1. **Incremental versions**: Use sequential numbers (001, 002, 003)
2. **Idempotent SQL**: Use `IF NOT EXISTS` and `IF EXISTS` clauses
3. **Transactional**: Keep migrations atomic (all or nothing)
4. **Backwards compatible**: Avoid breaking changes when possible
5. **Test thoroughly**: Test on fresh databases and with existing data
6. **Document schema changes**: Update docs and CHANGELOG

## Troubleshooting

### Migration Failed

If a migration fails:

```bash
# Check migration status
infer migrate --status

# Review error message
infer migrate
```

Common issues:

- **Database locked**: Close all CLI instances
- **Permission denied**: Check file/directory permissions
- **Syntax error**: Review migration SQL
- **Constraint violation**: Check for existing data conflicts

### Reset Database

To start fresh (⚠️ **destroys all data**):

```bash
# SQLite (default)
rm ~/.infer/conversations.db
infer migrate

# PostgreSQL
psql -c "DROP DATABASE infer_gateway; CREATE DATABASE infer_gateway;"
infer migrate
```

### Manual Intervention

If automatic migration fails, you can apply migrations manually:

```bash
# SQLite
sqlite3 ~/.infer/conversations.db < migration.sql

# PostgreSQL
psql infer_gateway < migration.sql
```

## Schema Versioning

The current schema version can be determined by:

```bash
# Show applied migrations
infer migrate --status

# Query directly (SQLite)
sqlite3 ~/.infer/conversations.db "SELECT version, description, applied_at FROM schema_migrations ORDER BY version;"

# Query directly (PostgreSQL)
psql infer_gateway -c "SELECT version, description, applied_at FROM schema_migrations ORDER BY version;"
```

## Migration History

### Version 001 (Initial Schema)

**SQLite**:

- Created `conversations` table with all required columns
- Added index on `updated_at` for efficient queries

**PostgreSQL**:

- Created `conversations` table with JSONB columns
- Created `conversation_entries` table with foreign key
- Added indexes for efficient queries

## Future Enhancements

Potential improvements to the migration system:

- [ ] Migration dry-run mode
- [ ] Migration rollback support
- [ ] Migration file generator CLI command
- [ ] Migration lint/validation tool
- [ ] Data migration helpers (not just schema)
- [ ] Multi-step migration with progress reporting

## See Also

- [Storage Architecture](./storage-architecture.md) (if exists)
- [Configuration Guide](../CLAUDE.md)
- [Upgrade Guide](../CHANGELOG.md)
