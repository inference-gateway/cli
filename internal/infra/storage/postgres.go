package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	_ "github.com/lib/pq"
)

// PostgresStorage implements ConversationStorage using PostgreSQL
type PostgresStorage struct {
	db *sql.DB
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
	if err := verifyPostgresAvailable(config); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.Username, config.Password, config.Database, config.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open PostgreSQL connection: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	storage := &PostgresStorage{db: db}

	if err := storage.createTables(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return storage, nil
}

// createTables creates the necessary tables for conversation storage
func (s *PostgresStorage) createTables(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS conversations (
		id VARCHAR(255) PRIMARY KEY,
		title TEXT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE NOT NULL,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
		message_count INTEGER NOT NULL DEFAULT 0,
		model VARCHAR(255),
		tags JSONB,
		summary TEXT,
		optimized_messages JSONB,
		token_stats JSONB,
		cost_stats JSONB,
		title_generated BOOLEAN DEFAULT FALSE,
		title_invalidated BOOLEAN DEFAULT FALSE,
		title_generation_time TIMESTAMP WITH TIME ZONE
	);

	CREATE TABLE IF NOT EXISTS conversation_entries (
		id BIGSERIAL PRIMARY KEY,
		conversation_id VARCHAR(255) NOT NULL,
		entry_data JSONB NOT NULL,
		sequence_number INTEGER NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE NOT NULL,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_conversations_created_at ON conversations(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_conversation_entries_conversation_id ON conversation_entries(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_conversation_entries_sequence ON conversation_entries(conversation_id, sequence_number);
	CREATE INDEX IF NOT EXISTS idx_conversations_tags ON conversations USING gin(tags);
	CREATE INDEX IF NOT EXISTS idx_conversations_title_invalidated ON conversations(title_invalidated, title_generated);
	`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}

	migrationSchema := `
	ALTER TABLE conversations ADD COLUMN IF NOT EXISTS title_generated BOOLEAN DEFAULT FALSE;
	ALTER TABLE conversations ADD COLUMN IF NOT EXISTS title_invalidated BOOLEAN DEFAULT FALSE;
	ALTER TABLE conversations ADD COLUMN IF NOT EXISTS title_generation_time TIMESTAMP WITH TIME ZONE;
	ALTER TABLE conversations ADD COLUMN IF NOT EXISTS optimized_messages JSONB;
	ALTER TABLE conversations ADD COLUMN IF NOT EXISTS cost_stats JSONB;
	`

	_, _ = s.db.ExecContext(ctx, migrationSchema)

	return nil
}

// SaveConversation saves a conversation with its entries
func (s *PostgresStorage) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	tokenStatsJSON, err := json.Marshal(metadata.TokenStats)
	if err != nil {
		return fmt.Errorf("failed to marshal token stats: %w", err)
	}

	tagsJSON, err := json.Marshal(metadata.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	costStatsJSON, err := json.Marshal(metadata.CostStats)
	if err != nil {
		return fmt.Errorf("failed to marshal cost stats: %w", err)
	}

	var optimizedMessagesJSON []byte
	if len(metadata.OptimizedMessages) > 0 {
		optimizedMessagesJSON, err = json.Marshal(metadata.OptimizedMessages)
		if err != nil {
			return fmt.Errorf("failed to marshal optimized messages: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO conversations (id, title, created_at, updated_at, message_count, model, tags, summary, optimized_messages, token_stats, cost_stats, title_generated, title_invalidated, title_generation_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT(id) DO UPDATE SET
			title = EXCLUDED.title,
			updated_at = EXCLUDED.updated_at,
			message_count = EXCLUDED.message_count,
			model = EXCLUDED.model,
			tags = EXCLUDED.tags,
			summary = EXCLUDED.summary,
			optimized_messages = EXCLUDED.optimized_messages,
			token_stats = EXCLUDED.token_stats,
			cost_stats = EXCLUDED.cost_stats,
			title_generated = EXCLUDED.title_generated,
			title_invalidated = EXCLUDED.title_invalidated,
			title_generation_time = EXCLUDED.title_generation_time
	`, conversationID, metadata.Title, metadata.CreatedAt, metadata.UpdatedAt, len(entries), metadata.Model, string(tagsJSON), metadata.Summary, optimizedMessagesJSON, string(tokenStatsJSON), string(costStatsJSON), metadata.TitleGenerated, metadata.TitleInvalidated, metadata.TitleGenerationTime)
	if err != nil {
		return fmt.Errorf("failed to save conversation metadata: %w", err)
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM conversation_entries WHERE conversation_id = $1", conversationID)
	if err != nil {
		return fmt.Errorf("failed to delete existing entries: %w", err)
	}

	for i, entry := range entries {
		entryJSON, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal entry %d: %w", i, err)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO conversation_entries (conversation_id, entry_data, sequence_number, created_at)
			VALUES ($1, $2, $3, $4)
		`, conversationID, string(entryJSON), i, entry.Time)
		if err != nil {
			return fmt.Errorf("failed to save entry %d: %w", i, err)
		}
	}

	return tx.Commit()
}

// LoadConversation loads a conversation by its ID
func (s *PostgresStorage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
	var metadata ConversationMetadata
	var tokenStatsJSON, tagsJSON, costStatsJSON string
	var optimizedMessagesJSON sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, created_at, updated_at, message_count, model, tags, summary, optimized_messages, token_stats, COALESCE(cost_stats, '{}'),
			   COALESCE(title_generated, FALSE), COALESCE(title_invalidated, FALSE), title_generation_time
		FROM conversations WHERE id = $1
	`, conversationID).Scan(
		&metadata.ID, &metadata.Title, &metadata.CreatedAt, &metadata.UpdatedAt,
		&metadata.MessageCount, &metadata.Model, &tagsJSON, &metadata.Summary, &optimizedMessagesJSON, &tokenStatsJSON, &costStatsJSON,
		&metadata.TitleGenerated, &metadata.TitleInvalidated, &metadata.TitleGenerationTime,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, metadata, fmt.Errorf("conversation not found: %s", conversationID)
		}
		return nil, metadata, fmt.Errorf("failed to load conversation metadata: %w", err)
	}

	if err := json.Unmarshal([]byte(tokenStatsJSON), &metadata.TokenStats); err != nil {
		return nil, metadata, fmt.Errorf("failed to unmarshal token stats: %w", err)
	}

	if costStatsJSON != "" && costStatsJSON != "{}" {
		if err := json.Unmarshal([]byte(costStatsJSON), &metadata.CostStats); err != nil {
			metadata.CostStats = domain.SessionCostStats{}
		}
	}

	if err := json.Unmarshal([]byte(tagsJSON), &metadata.Tags); err != nil {
		return nil, metadata, fmt.Errorf("failed to unmarshal tags: %w", err)
	}

	if optimizedMessagesJSON.Valid && optimizedMessagesJSON.String != "" {
		if err := json.Unmarshal([]byte(optimizedMessagesJSON.String), &metadata.OptimizedMessages); err != nil {
			return nil, metadata, fmt.Errorf("failed to unmarshal optimized messages: %w", err)
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_data FROM conversation_entries
		WHERE conversation_id = $1
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
func (s *PostgresStorage) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, message_count, model, tags, summary, token_stats, COALESCE(cost_stats, '{}'),
			   COALESCE(title_generated, FALSE), COALESCE(title_invalidated, FALSE), title_generation_time
		FROM conversations
		ORDER BY updated_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []ConversationSummary
	for rows.Next() {
		var summary ConversationSummary
		var tokenStatsJSON, tagsJSON, costStatsJSON string

		err := rows.Scan(
			&summary.ID, &summary.Title, &summary.CreatedAt, &summary.UpdatedAt,
			&summary.MessageCount, &summary.Model, &tagsJSON, &summary.Summary, &tokenStatsJSON, &costStatsJSON,
			&summary.TitleGenerated, &summary.TitleInvalidated, &summary.TitleGenerationTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}

		if err := json.Unmarshal([]byte(tokenStatsJSON), &summary.TokenStats); err != nil {
			return nil, fmt.Errorf("failed to unmarshal token stats: %w", err)
		}

		if costStatsJSON != "" && costStatsJSON != "{}" {
			if err := json.Unmarshal([]byte(costStatsJSON), &summary.CostStats); err != nil {
				summary.CostStats = domain.SessionCostStats{}
			}
		}

		if err := json.Unmarshal([]byte(tagsJSON), &summary.Tags); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
		}

		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

// ListConversationsNeedingTitles returns conversations that need title generation
func (s *PostgresStorage) ListConversationsNeedingTitles(ctx context.Context, limit int) ([]ConversationSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, message_count, model, tags, summary, token_stats, COALESCE(cost_stats, '{}'),
			   COALESCE(title_generated, FALSE), COALESCE(title_invalidated, FALSE), title_generation_time
		FROM conversations
		WHERE (COALESCE(title_generated, FALSE) = FALSE OR COALESCE(title_invalidated, FALSE) = TRUE)
		  AND message_count > 0
		ORDER BY updated_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query conversations needing titles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []ConversationSummary
	for rows.Next() {
		var summary ConversationSummary
		var tokenStatsJSON, tagsJSON, costStatsJSON string

		err := rows.Scan(
			&summary.ID, &summary.Title, &summary.CreatedAt, &summary.UpdatedAt,
			&summary.MessageCount, &summary.Model, &tagsJSON, &summary.Summary, &tokenStatsJSON, &costStatsJSON,
			&summary.TitleGenerated, &summary.TitleInvalidated, &summary.TitleGenerationTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}

		if err := json.Unmarshal([]byte(tokenStatsJSON), &summary.TokenStats); err != nil {
			return nil, fmt.Errorf("failed to unmarshal token stats: %w", err)
		}

		if costStatsJSON != "" && costStatsJSON != "{}" {
			if err := json.Unmarshal([]byte(costStatsJSON), &summary.CostStats); err != nil {
				summary.CostStats = domain.SessionCostStats{}
			}
		}

		if err := json.Unmarshal([]byte(tagsJSON), &summary.Tags); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
		}

		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

// DeleteConversation removes a conversation by its ID
func (s *PostgresStorage) DeleteConversation(ctx context.Context, conversationID string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM conversations WHERE id = $1", conversationID)
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
func (s *PostgresStorage) UpdateConversationMetadata(ctx context.Context, conversationID string, metadata ConversationMetadata) error {
	tokenStatsJSON, err := json.Marshal(metadata.TokenStats)
	if err != nil {
		return fmt.Errorf("failed to marshal token stats: %w", err)
	}

	tagsJSON, err := json.Marshal(metadata.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	costStatsJSON, err := json.Marshal(metadata.CostStats)
	if err != nil {
		return fmt.Errorf("failed to marshal cost stats: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE conversations
		SET title = $1, updated_at = $2, model = $3, tags = $4, summary = $5, token_stats = $6, cost_stats = $7,
		    title_generated = $8, title_invalidated = $9, title_generation_time = $10
		WHERE id = $11
	`, metadata.Title, metadata.UpdatedAt, metadata.Model, string(tagsJSON), metadata.Summary, string(tokenStatsJSON), string(costStatsJSON), metadata.TitleGenerated, metadata.TitleInvalidated, metadata.TitleGenerationTime, conversationID)
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
func (s *PostgresStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Health checks if the database is reachable and functional
func (s *PostgresStorage) Health(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	var result int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		return fmt.Errorf("database query test failed: %w", err)
	}

	return nil
}
