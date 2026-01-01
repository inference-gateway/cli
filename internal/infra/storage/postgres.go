package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	migrations "github.com/inference-gateway/cli/internal/infra/storage/migrations"

	_ "github.com/lib/pq"
)

// PostgresStorage implements ConversationStorage using PostgreSQL
type PostgresStorage struct {
	db *sql.DB
}

// DB returns the underlying database connection
func (s *PostgresStorage) DB() *sql.DB {
	return s.db
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

	if err := storage.runMigrations(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return storage, nil
}

// runMigrations applies all pending database migrations
func (s *PostgresStorage) runMigrations(ctx context.Context) error {
	runner := migrations.NewMigrationRunner(s.db, "postgres")

	// Get all PostgreSQL migrations
	allMigrations := migrations.GetPostgresMigrations()

	// Apply migrations
	appliedCount, err := runner.ApplyMigrations(ctx, allMigrations)
	if err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	// Log applied migrations count (only if any were applied)
	if appliedCount > 0 {
		// Migrations were applied, but we don't log here as this is a library
		_ = appliedCount
	}

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

	_, err = tx.ExecContext(ctx, `
		INSERT INTO conversations (id, title, created_at, updated_at, message_count, model, tags, token_stats, cost_stats, title_generated, title_invalidated, title_generation_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT(id) DO UPDATE SET
			title = EXCLUDED.title,
			updated_at = EXCLUDED.updated_at,
			message_count = EXCLUDED.message_count,
			model = EXCLUDED.model,
			tags = EXCLUDED.tags,
			token_stats = EXCLUDED.token_stats,
			cost_stats = EXCLUDED.cost_stats,
			title_generated = EXCLUDED.title_generated,
			title_invalidated = EXCLUDED.title_invalidated,
			title_generation_time = EXCLUDED.title_generation_time
	`, conversationID, metadata.Title, metadata.CreatedAt, metadata.UpdatedAt, len(entries), metadata.Model, string(tagsJSON), string(tokenStatsJSON), string(costStatsJSON), metadata.TitleGenerated, metadata.TitleInvalidated, metadata.TitleGenerationTime)
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

	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, created_at, updated_at, message_count, model, tags, token_stats, COALESCE(cost_stats, '{}'),
			   COALESCE(title_generated, FALSE), COALESCE(title_invalidated, FALSE), title_generation_time
		FROM conversations WHERE id = $1
	`, conversationID).Scan(
		&metadata.ID, &metadata.Title, &metadata.CreatedAt, &metadata.UpdatedAt,
		&metadata.MessageCount, &metadata.Model, &tagsJSON, &tokenStatsJSON, &costStatsJSON,
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
			&summary.MessageCount, &summary.Model, &tagsJSON, &tokenStatsJSON, &costStatsJSON,
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
			&summary.MessageCount, &summary.Model, &tagsJSON, &tokenStatsJSON, &costStatsJSON,
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
		SET title = $1, updated_at = $2, model = $3, tags = $4, token_stats = $5, cost_stats = $6,
		    title_generated = $7, title_invalidated = $8, title_generation_time = $9
		WHERE id = $10
	`, metadata.Title, metadata.UpdatedAt, metadata.Model, string(tagsJSON), string(tokenStatsJSON), string(costStatsJSON), metadata.TitleGenerated, metadata.TitleInvalidated, metadata.TitleGenerationTime, conversationID)
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
