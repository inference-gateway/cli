package infrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	migrations "github.com/inference-gateway/cli/internal/platform/storage/migrations"
	storage "github.com/inference-gateway/cli/internal/platform/storage"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// sqlQueueBackend implements queueBackend over a SQL database: one
// message_queue table shared by the SQLite and PostgreSQL adapters, differing
// only in placeholders and locking hints. One dialect string selects both.
type sqlQueueBackend struct {
	db      *sql.DB
	dialect string
}

// NewSQLiteMessageQueue opens the storage.sqlite database, applies the schema
// migrations and returns the durable queue adapter. Startup fails fast with an
// error naming the backend when the database file is unusable.
func NewSQLiteMessageQueue(sqliteConn storage.SQLiteConfig, name func() string, interval time.Duration, wake func()) (convdomain.MessageQueue, error) {
	db, err := storage.OpenSQLite(sqliteConn.Path)
	if err != nil {
		return nil, fmt.Errorf("queue backend sqlite: %w", err)
	}

	if _, err := applySQLMigrations(db, "sqlite"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("queue backend sqlite: %w", err)
	}

	return newDurableMessageQueue("sqlite", &sqlQueueBackend{db: db, dialect: "sqlite"}, name, interval, wake), nil
}

// NewPostgresMessageQueue dials the storage.postgres connection, applies the
// schema migrations and returns the durable queue adapter. Startup fails fast
// with an error naming the backend when PostgreSQL is unreachable.
func NewPostgresMessageQueue(pgConn storage.PostgresConfig, name func() string, interval time.Duration, wake func()) (convdomain.MessageQueue, error) {
	db, err := storage.OpenPostgres(pgConn)
	if err != nil {
		return nil, fmt.Errorf("queue backend postgres: %w", err)
	}

	if _, err := applySQLMigrations(db, "postgres"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("queue backend postgres: %w", err)
	}

	return newDurableMessageQueue("postgres", &sqlQueueBackend{db: db, dialect: "postgres"}, name, interval, wake), nil
}

// applySQLMigrations brings the shared SQL schema up to date, idempotent per dialect.
func applySQLMigrations(db *sql.DB, dialect string) (int, error) {
	set := migrations.GetSQLiteMigrations()
	if dialect == "postgres" {
		set = migrations.GetPostgresMigrations()
	}
	return migrations.NewMigrationRunner(db, dialect).ApplyMigrations(context.Background(), set)
}

// placeholderSQL rewrites ? placeholders into $N slots for PostgreSQL; SQLite
// keeps them as-is.
func (b *sqlQueueBackend) placeholderSQL(query string) string {
	if b.dialect == "sqlite" {
		return query
	}

	var rewritten strings.Builder
	ordinal := 0
	for _, char := range query {
		if char == '?' {
			ordinal++
			rewritten.WriteString("$" + strconv.Itoa(ordinal))
			continue
		}
		rewritten.WriteRune(char)
	}
	return rewritten.String()
}

func (b *sqlQueueBackend) enqueue(ctx context.Context, name string, record queuedRecord) error {
	_, err := b.db.ExecContext(ctx, b.placeholderSQL(`
		INSERT INTO message_queue (queue_name, role, content, source, request_id, queued_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`), name, record.Role, string(record.Content), record.Source, record.RequestID, record.QueuedAt)
	if err != nil {
		return fmt.Errorf("inserting queued message: %w", err)
	}
	return nil
}

// drain removes and returns every record of the queue in one statement: SQLite
// runs the delete inside its implicit single-statement transaction, PostgreSQL
// locks exactly the rows it deletes so two workers never take the same record.
func (b *sqlQueueBackend) drain(ctx context.Context, name string) ([]queuedRecord, error) {
	query := `
		DELETE FROM message_queue
		WHERE queue_name = ?
		RETURNING id, role, content, source, request_id, queued_at
	`
	if b.dialect == "postgres" {
		query = `
			DELETE FROM message_queue
			WHERE id IN (
				SELECT id FROM message_queue
				WHERE queue_name = ?
				ORDER BY id
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, role, content, source, request_id, queued_at
		`
	}
	return b.queryRecords(ctx, b.placeholderSQL(query), name)
}

// dequeue removes and returns the head record, or nil when the queue is empty.
func (b *sqlQueueBackend) dequeue(ctx context.Context, name string) (*queuedRecord, error) {
	records, err := b.queryRecords(ctx, b.placeholderSQL(`
		DELETE FROM message_queue
		WHERE queue_name = ? AND id IN (
			SELECT id FROM message_queue
			WHERE queue_name = ?
			ORDER BY id
			LIMIT 1
		)
		RETURNING id, role, content, source, request_id, queued_at
	`), name, name)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	return &records[0], nil
}

func (b *sqlQueueBackend) clear(ctx context.Context, name string) error {
	_, err := b.db.ExecContext(ctx, b.placeholderSQL(`
		DELETE FROM message_queue WHERE queue_name = ?
	`), name)
	if err != nil {
		return fmt.Errorf("clearing queued messages: %w", err)
	}
	return nil
}

// list reads every record of the queue without removing them (poll reads).
func (b *sqlQueueBackend) list(ctx context.Context, name string) ([]queuedRecord, error) {
	return b.queryRecords(ctx, b.placeholderSQL(`
		SELECT id, role, content, source, request_id, queued_at
		FROM message_queue
		WHERE queue_name = ?
		ORDER BY id
	`), name)
}

// queryRecords runs a record-returning query and scans every row.
func (b *sqlQueueBackend) queryRecords(ctx context.Context, query string, args ...any) ([]queuedRecord, error) {
	rows, err := b.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("queue query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	records := []queuedRecord{}
	for rows.Next() {
		record, err := scanQueuedRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queue rows: %w", err)
	}
	return records, nil
}

// scanQueuedRecord reads one row of the message_queue table.
func scanQueuedRecord(row interface{ Scan(...any) error }) (queuedRecord, error) {
	var record queuedRecord
	var content string
	if err := row.Scan(&record.Seq, &record.Role, &content, &record.Source, &record.RequestID, &record.QueuedAt); err != nil {
		return queuedRecord{}, fmt.Errorf("scanning queued message: %w", err)
	}
	record.Content = json.RawMessage(content)
	return record, nil
}