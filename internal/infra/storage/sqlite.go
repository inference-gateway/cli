package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	_ "modernc.org/sqlite"
)

// SQLiteStorage implements ConversationStorage using SQLite
type SQLiteStorage struct {
	db   *sql.DB
	path string
}

// NewSQLiteStorage creates a new SQLite storage instance
func NewSQLiteStorage(config SQLiteConfig) (*SQLiteStorage, error) {
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

// createTables creates the simplified single-table conversation storage
func (s *SQLiteStorage) createTables() error {
	var hasCorrectSchema int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='conversations'
		AND sql LIKE '%messages TEXT NOT NULL%'
		AND sql LIKE '%models TEXT%'
		AND sql LIKE '%tags TEXT%'
		AND sql LIKE '%summary TEXT%'
	`).Scan(&hasCorrectSchema)
	if err != nil {
		return err
	}

	if hasCorrectSchema > 0 {
		return nil
	}

	newSchema := `
	CREATE TABLE IF NOT EXISTS conversations_new (
		id TEXT PRIMARY KEY,                -- Session ID
		title TEXT NOT NULL,               -- Conversation title
		count INTEGER NOT NULL DEFAULT 0,  -- Message count
		messages TEXT NOT NULL,            -- JSON array of all messages
		optimized_messages TEXT,           -- JSON array of optimized messages
		total_input_tokens INTEGER NOT NULL DEFAULT 0,   -- Total input tokens used
		total_output_tokens INTEGER NOT NULL DEFAULT 0,  -- Total output tokens used
		request_count INTEGER NOT NULL DEFAULT 0,        -- Number of API requests made
		cost_stats TEXT DEFAULT '{}',      -- JSON object of cost statistics
		models TEXT DEFAULT '[]',          -- JSON array of models used
		tags TEXT DEFAULT '[]',            -- JSON array of tags
		summary TEXT DEFAULT '',           -- Conversation summary
		title_generated BOOLEAN DEFAULT FALSE,
		title_invalidated BOOLEAN DEFAULT FALSE,
		title_generation_time DATETIME,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_conversations_new_updated_at ON conversations_new(updated_at DESC);
	`

	if _, err := s.db.Exec(newSchema); err != nil {
		return err
	}

	migrationQuery := `
	INSERT OR IGNORE INTO conversations_new (id, title, count, messages, optimized_messages, total_input_tokens, total_output_tokens, created_at, updated_at)
	SELECT
		c.id,
		c.title,
		c.message_count,
		'[' || GROUP_CONCAT(ce.entry_data) || ']' as messages,
		NULL as optimized_messages,  -- No optimized messages for migrated data
		0 as total_input_tokens,     -- Start with 0 for migrated data
		0 as total_output_tokens,    -- Start with 0 for migrated data
		c.created_at,
		c.updated_at
	FROM conversations c
	LEFT JOIN conversation_entries ce ON c.id = ce.conversation_id
	WHERE EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='conversations')
	GROUP BY c.id, c.title, c.message_count, c.created_at, c.updated_at;
	`

	_, _ = s.db.Exec(migrationQuery)

	renameSchema := `
	DROP TABLE IF EXISTS conversations;
	ALTER TABLE conversations_new RENAME TO conversations;
	`

	if _, err := s.db.Exec(renameSchema); err != nil {
		return err
	}

	return nil
}

// SaveConversation saves a conversation with its entries using simplified schema
func (s *SQLiteStorage) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	modelsUsed := make(map[string]bool)

	for _, entry := range entries {
		if entry.Model != "" {
			modelsUsed[entry.Model] = true
		}
	}

	messagesJSON, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}

	var models []string
	for model := range modelsUsed {
		models = append(models, model)
	}
	modelsJSON, err := json.Marshal(models)
	if err != nil {
		return fmt.Errorf("failed to marshal models: %w", err)
	}

	tagsJSON, err := json.Marshal(metadata.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	var optimizedMessagesJSON []byte
	if len(metadata.OptimizedMessages) > 0 {
		optimizedMessagesJSON, err = json.Marshal(metadata.OptimizedMessages)
		if err != nil {
			return fmt.Errorf("failed to marshal optimized messages: %w", err)
		}
	}

	totalInputTokens := metadata.TokenStats.TotalInputTokens
	totalOutputTokens := metadata.TokenStats.TotalOutputTokens
	requestCount := metadata.TokenStats.RequestCount

	costStatsJSON, err := json.Marshal(metadata.CostStats)
	if err != nil {
		return fmt.Errorf("failed to marshal cost stats: %w", err)
	}

	var optimizedMessagesStr *string
	if len(optimizedMessagesJSON) > 0 {
		str := string(optimizedMessagesJSON)
		optimizedMessagesStr = &str
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO conversations (id, title, count, messages, optimized_messages, total_input_tokens, total_output_tokens,
		                          request_count, cost_stats, models, tags, summary, title_generated, title_invalidated, title_generation_time,
		                          created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			count = excluded.count,
			messages = excluded.messages,
			optimized_messages = excluded.optimized_messages,
			total_input_tokens = excluded.total_input_tokens,
			total_output_tokens = excluded.total_output_tokens,
			request_count = excluded.request_count,
			cost_stats = excluded.cost_stats,
			models = excluded.models,
			tags = excluded.tags,
			summary = excluded.summary,
			title_generated = excluded.title_generated,
			title_invalidated = excluded.title_invalidated,
			title_generation_time = excluded.title_generation_time,
			updated_at = excluded.updated_at
	`, conversationID, metadata.Title, len(entries), string(messagesJSON), optimizedMessagesStr, totalInputTokens, totalOutputTokens,
		requestCount, string(costStatsJSON), string(modelsJSON), string(tagsJSON), metadata.Summary, metadata.TitleGenerated, metadata.TitleInvalidated,
		metadata.TitleGenerationTime, metadata.CreatedAt.Format(time.RFC3339), metadata.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to save conversation: %w", err)
	}

	return nil
}

// LoadConversation loads a conversation by its ID using simplified schema
func (s *SQLiteStorage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
	metadata, messagesJSON, optimizedMessagesJSON, err := s.loadConversationMetadata(ctx, conversationID)
	if err != nil {
		return nil, metadata, err
	}

	if optimizedMessagesJSON.Valid && optimizedMessagesJSON.String != "" {
		if err := json.Unmarshal([]byte(optimizedMessagesJSON.String), &metadata.OptimizedMessages); err != nil {
			return nil, metadata, fmt.Errorf("failed to unmarshal optimized messages: %w", err)
		}
	}

	var entries []domain.ConversationEntry
	if err := json.Unmarshal([]byte(messagesJSON), &entries); err != nil {
		return nil, metadata, fmt.Errorf("failed to unmarshal messages: %w", err)
	}

	return entries, metadata, nil
}

// loadConversationMetadata loads the metadata for a conversation
func (s *SQLiteStorage) loadConversationMetadata(ctx context.Context, conversationID string) (ConversationMetadata, string, sql.NullString, error) {
	var metadata ConversationMetadata
	var messagesJSON, modelsJSON, tagsJSON, costStatsJSON string
	var optimizedMessagesJSON sql.NullString
	var totalInputTokens, totalOutputTokens, requestCount int
	var titleGenerationTime sql.NullTime

	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, count, messages, optimized_messages, total_input_tokens, total_output_tokens,
		       request_count, cost_stats, models, tags, summary, title_generated, title_invalidated, title_generation_time,
		       created_at, updated_at
		FROM conversations WHERE id = ?
	`, conversationID).Scan(
		&metadata.ID, &metadata.Title, &metadata.MessageCount,
		&messagesJSON, &optimizedMessagesJSON, &totalInputTokens, &totalOutputTokens,
		&requestCount, &costStatsJSON, &modelsJSON, &tagsJSON, &metadata.Summary,
		&metadata.TitleGenerated, &metadata.TitleInvalidated, &titleGenerationTime,
		&metadata.CreatedAt, &metadata.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return metadata, "", optimizedMessagesJSON, fmt.Errorf("conversation not found: %s", conversationID)
		}
		return metadata, "", optimizedMessagesJSON, fmt.Errorf("failed to load conversation: %w", err)
	}

	metadata.TokenStats = domain.SessionTokenStats{
		TotalInputTokens:  totalInputTokens,
		TotalOutputTokens: totalOutputTokens,
		TotalTokens:       totalInputTokens + totalOutputTokens,
		RequestCount:      requestCount,
	}

	if costStatsJSON != "" && costStatsJSON != "{}" {
		if err := json.Unmarshal([]byte(costStatsJSON), &metadata.CostStats); err != nil {
			metadata.CostStats = domain.SessionCostStats{}
		}
	}

	var models []string
	if modelsJSON != "" && modelsJSON != "[]" {
		if err := json.Unmarshal([]byte(modelsJSON), &models); err == nil && len(models) > 0 {
			metadata.Model = models[0]
		}
	}

	if tagsJSON != "" && tagsJSON != "[]" {
		_ = json.Unmarshal([]byte(tagsJSON), &metadata.Tags)
	}

	if titleGenerationTime.Valid {
		metadata.TitleGenerationTime = &titleGenerationTime.Time
	}

	return metadata, messagesJSON, optimizedMessagesJSON, nil
}

// ListConversations returns a list of conversation summaries
func (s *SQLiteStorage) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, count, total_input_tokens, total_output_tokens, request_count, cost_stats
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
		var totalInputTokens, totalOutputTokens, requestCount int
		var costStatsJSON string

		err := rows.Scan(
			&summary.ID, &summary.Title, &summary.CreatedAt, &summary.UpdatedAt,
			&summary.MessageCount, &totalInputTokens, &totalOutputTokens, &requestCount, &costStatsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}

		summary.TokenStats = domain.SessionTokenStats{
			TotalInputTokens:  totalInputTokens,
			TotalOutputTokens: totalOutputTokens,
			TotalTokens:       totalInputTokens + totalOutputTokens,
			RequestCount:      requestCount,
		}

		if costStatsJSON != "" && costStatsJSON != "{}" {
			if err := json.Unmarshal([]byte(costStatsJSON), &summary.CostStats); err != nil {
				summary.CostStats = domain.SessionCostStats{}
			}
		}

		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

// ListConversationsNeedingTitles returns conversations that need title generation
func (s *SQLiteStorage) ListConversationsNeedingTitles(ctx context.Context, limit int) ([]ConversationSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, count, total_input_tokens, total_output_tokens,
		       models, tags, summary, title_generated, title_invalidated, title_generation_time
		FROM conversations
		WHERE (title_generated = FALSE OR title_invalidated = TRUE)
		  AND count >= 2  -- Only conversations with at least 2 messages (user + assistant)
		ORDER BY updated_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query conversations needing titles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []ConversationSummary
	for rows.Next() {
		var summary ConversationSummary
		var totalInputTokens, totalOutputTokens int
		var modelsJSON, tagsJSON string
		var titleGenerationTime sql.NullTime

		err := rows.Scan(
			&summary.ID, &summary.Title, &summary.CreatedAt, &summary.UpdatedAt,
			&summary.MessageCount, &totalInputTokens, &totalOutputTokens,
			&modelsJSON, &tagsJSON, &summary.Summary,
			&summary.TitleGenerated, &summary.TitleInvalidated, &titleGenerationTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}

		summary.TokenStats = domain.SessionTokenStats{
			TotalInputTokens:  totalInputTokens,
			TotalOutputTokens: totalOutputTokens,
			TotalTokens:       totalInputTokens + totalOutputTokens,
			RequestCount:      summary.MessageCount / 2,
		}

		var models []string
		if modelsJSON != "" && modelsJSON != "[]" {
			if err := json.Unmarshal([]byte(modelsJSON), &models); err == nil && len(models) > 0 {
				summary.Model = models[0]
			}
		}

		if tagsJSON != "" && tagsJSON != "[]" {
			_ = json.Unmarshal([]byte(tagsJSON), &summary.Tags)
		}

		if titleGenerationTime.Valid {
			summary.TitleGenerationTime = &titleGenerationTime.Time
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
	tagsJSON, err := json.Marshal(metadata.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	costStatsJSON, err := json.Marshal(metadata.CostStats)
	if err != nil {
		return fmt.Errorf("failed to marshal cost stats: %w", err)
	}

	modelsJSON := "[]"
	if metadata.Model != "" {
		models := []string{metadata.Model}
		if modelsData, err := json.Marshal(models); err == nil {
			modelsJSON = string(modelsData)
		}
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE conversations
		SET title = ?, updated_at = ?, models = ?, tags = ?, summary = ?,
		    total_input_tokens = ?, total_output_tokens = ?, request_count = ?, cost_stats = ?,
		    title_generated = ?, title_invalidated = ?, title_generation_time = ?
		WHERE id = ?
	`, metadata.Title, metadata.UpdatedAt, modelsJSON, string(tagsJSON), metadata.Summary,
		metadata.TokenStats.TotalInputTokens, metadata.TokenStats.TotalOutputTokens, metadata.TokenStats.RequestCount, string(costStatsJSON),
		metadata.TitleGenerated, metadata.TitleInvalidated, metadata.TitleGenerationTime, conversationID)
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

	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	var result int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		return fmt.Errorf("database query test failed: %w", err)
	}

	return nil
}
