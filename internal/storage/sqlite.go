package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/inference-gateway/cli/internal/domain"
	_ "modernc.org/sqlite"
)

// SQLiteStorage implements ConversationStorage using SQLite
type SQLiteStorage struct {
	db   *sql.DB
	path string
}

// NewSQLiteStorage creates a new SQLite storage instance
func NewSQLiteStorage(config SQLiteConfig) (*SQLiteStorage, error) {
	// Ensure directory exists
	dir := filepath.Dir(config.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	storage := &SQLiteStorage{
		db:   db,
		path: config.Path,
	}

	if err := storage.createTables(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return storage, nil
}

// createTables creates the necessary tables for conversation storage
func (s *SQLiteStorage) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS conversations (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		message_count INTEGER NOT NULL DEFAULT 0,
		model TEXT,
		tags TEXT, -- JSON array
		summary TEXT,
		token_stats TEXT -- JSON object
	);

	CREATE TABLE IF NOT EXISTS conversation_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id TEXT NOT NULL,
		entry_data TEXT NOT NULL, -- JSON serialized ConversationEntry
		sequence_number INTEGER NOT NULL,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_conversations_created_at ON conversations(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_conversation_entries_conversation_id ON conversation_entries(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_conversation_entries_sequence ON conversation_entries(conversation_id, sequence_number);
	`

	_, err := s.db.Exec(schema)
	return err
}

// SaveConversation saves a conversation with its entries
func (s *SQLiteStorage) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize metadata
	tokenStatsJSON, err := json.Marshal(metadata.TokenStats)
	if err != nil {
		return fmt.Errorf("failed to marshal token stats: %w", err)
	}

	tagsJSON, err := json.Marshal(metadata.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	// Upsert conversation metadata
	_, err = tx.ExecContext(ctx, `
		INSERT INTO conversations (id, title, created_at, updated_at, message_count, model, tags, summary, token_stats)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			updated_at = excluded.updated_at,
			message_count = excluded.message_count,
			model = excluded.model,
			tags = excluded.tags,
			summary = excluded.summary,
			token_stats = excluded.token_stats
	`, conversationID, metadata.Title, metadata.CreatedAt, metadata.UpdatedAt, len(entries), metadata.Model, string(tagsJSON), metadata.Summary, string(tokenStatsJSON))
	if err != nil {
		return fmt.Errorf("failed to save conversation metadata: %w", err)
	}

	// Delete existing entries for this conversation
	_, err = tx.ExecContext(ctx, "DELETE FROM conversation_entries WHERE conversation_id = ?", conversationID)
	if err != nil {
		return fmt.Errorf("failed to delete existing entries: %w", err)
	}

	// Insert new entries
	for i, entry := range entries {
		entryJSON, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal entry %d: %w", i, err)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO conversation_entries (conversation_id, entry_data, sequence_number, created_at)
			VALUES (?, ?, ?, ?)
		`, conversationID, string(entryJSON), i, entry.Time)
		if err != nil {
			return fmt.Errorf("failed to save entry %d: %w", i, err)
		}
	}

	return tx.Commit()
}

// LoadConversation loads a conversation by its ID
func (s *SQLiteStorage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
	var metadata ConversationMetadata
	var tokenStatsJSON, tagsJSON string

	// Load conversation metadata
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, created_at, updated_at, message_count, model, tags, summary, token_stats
		FROM conversations WHERE id = ?
	`, conversationID).Scan(
		&metadata.ID, &metadata.Title, &metadata.CreatedAt, &metadata.UpdatedAt,
		&metadata.MessageCount, &metadata.Model, &tagsJSON, &metadata.Summary, &tokenStatsJSON,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, metadata, fmt.Errorf("conversation not found: %s", conversationID)
		}
		return nil, metadata, fmt.Errorf("failed to load conversation metadata: %w", err)
	}

	// Unmarshal metadata
	if err := json.Unmarshal([]byte(tokenStatsJSON), &metadata.TokenStats); err != nil {
		return nil, metadata, fmt.Errorf("failed to unmarshal token stats: %w", err)
	}

	if err := json.Unmarshal([]byte(tagsJSON), &metadata.Tags); err != nil {
		return nil, metadata, fmt.Errorf("failed to unmarshal tags: %w", err)
	}

	// Load conversation entries
	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_data FROM conversation_entries 
		WHERE conversation_id = ? 
		ORDER BY sequence_number ASC
	`, conversationID)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to query entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []domain.ConversationEntry
	for rows.Next() {
		var entryJSON string
		if err := rows.Scan(&entryJSON); err != nil {
			return nil, metadata, fmt.Errorf("failed to scan entry: %w", err)
		}

		var entry domain.ConversationEntry
		if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
			return nil, metadata, fmt.Errorf("failed to unmarshal entry: %w", err)
		}

		entries = append(entries, entry)
	}

	return entries, metadata, rows.Err()
}

// ListConversations returns a list of conversation summaries
func (s *SQLiteStorage) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, message_count, model, tags, summary, token_stats
		FROM conversations 
		ORDER BY updated_at DESC 
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []ConversationSummary
	for rows.Next() {
		var summary ConversationSummary
		var tokenStatsJSON, tagsJSON string

		err := rows.Scan(
			&summary.ID, &summary.Title, &summary.CreatedAt, &summary.UpdatedAt,
			&summary.MessageCount, &summary.Model, &tagsJSON, &summary.Summary, &tokenStatsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal([]byte(tokenStatsJSON), &summary.TokenStats); err != nil {
			return nil, fmt.Errorf("failed to unmarshal token stats: %w", err)
		}

		if err := json.Unmarshal([]byte(tagsJSON), &summary.Tags); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
		}

		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

// DeleteConversation removes a conversation by its ID
func (s *SQLiteStorage) DeleteConversation(ctx context.Context, conversationID string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM conversations WHERE id = ?", conversationID)
	if err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}

	return nil
}

// UpdateConversationMetadata updates metadata for a conversation
func (s *SQLiteStorage) UpdateConversationMetadata(ctx context.Context, conversationID string, metadata ConversationMetadata) error {
	tokenStatsJSON, err := json.Marshal(metadata.TokenStats)
	if err != nil {
		return fmt.Errorf("failed to marshal token stats: %w", err)
	}

	tagsJSON, err := json.Marshal(metadata.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE conversations 
		SET title = ?, updated_at = ?, model = ?, tags = ?, summary = ?, token_stats = ?
		WHERE id = ?
	`, metadata.Title, metadata.UpdatedAt, metadata.Model, string(tagsJSON), metadata.Summary, string(tokenStatsJSON), conversationID)
	if err != nil {
		return fmt.Errorf("failed to update conversation metadata: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Health checks if the database is reachable and functional
func (s *SQLiteStorage) Health(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Simple ping test
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	// Test basic query
	var result int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		return fmt.Errorf("database query test failed: %w", err)
	}

	return nil
}
