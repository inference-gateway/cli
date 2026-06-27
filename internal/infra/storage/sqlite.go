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
	migrations "github.com/inference-gateway/cli/internal/infra/storage/migrations"
	_ "modernc.org/sqlite"
)

// SQLiteStorage implements ConversationStorage using SQLite
type SQLiteStorage struct {
	db   *sql.DB
	path string
}

// DB returns the underlying database connection
func (s *SQLiteStorage) DB() *sql.DB {
	return s.db
}

// NewSQLiteStorage creates a new SQLite storage instance
func NewSQLiteStorage(config SQLiteConfig) (*SQLiteStorage, error) {
	if err := verifySQLiteAvailable(); err != nil {
		return nil, err
	}

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

	if err := storage.runMigrations(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return storage, nil
}

// runMigrations applies all pending database migrations
func (s *SQLiteStorage) runMigrations() error {
	ctx := context.Background()
	runner := migrations.NewMigrationRunner(s.db, "sqlite")

	allMigrations := migrations.GetSQLiteMigrations()

	if _, err := runner.ApplyMigrations(ctx, allMigrations); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

// verifySQLiteAvailable checks if SQLite is available (using pure Go implementation)
func verifySQLiteAvailable() error {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return fmt.Errorf("SQLite driver not available (pure Go implementation): %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("SQLite connection test failed: %w", err)
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

	totalInputTokens := metadata.TokenStats.TotalInputTokens
	totalOutputTokens := metadata.TokenStats.TotalOutputTokens
	requestCount := metadata.TokenStats.RequestCount

	costStatsJSON, err := json.Marshal(metadata.CostStats)
	if err != nil {
		return fmt.Errorf("failed to marshal cost stats: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO conversations (id, title, count, messages, total_input_tokens, total_output_tokens,
		                          request_count, cost_stats, models, tags, title_generated, title_invalidated, title_generation_time,
		                          created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			count = excluded.count,
			messages = excluded.messages,
			total_input_tokens = excluded.total_input_tokens,
			total_output_tokens = excluded.total_output_tokens,
			request_count = excluded.request_count,
			cost_stats = excluded.cost_stats,
			models = excluded.models,
			tags = excluded.tags,
			title_generated = excluded.title_generated,
			title_invalidated = excluded.title_invalidated,
			title_generation_time = excluded.title_generation_time,
			updated_at = excluded.updated_at
	`, conversationID, metadata.Title, len(entries), string(messagesJSON), totalInputTokens, totalOutputTokens,
		requestCount, string(costStatsJSON), string(modelsJSON), string(tagsJSON), metadata.TitleGenerated, metadata.TitleInvalidated,
		metadata.TitleGenerationTime, metadata.CreatedAt.Format(time.RFC3339), metadata.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to save conversation: %w", err)
	}

	return nil
}

// LoadConversation loads a conversation by its ID using simplified schema
func (s *SQLiteStorage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
	metadata, messagesJSON, err := s.loadConversationMetadata(ctx, conversationID)
	if err != nil {
		return nil, metadata, err
	}

	var entries []domain.ConversationEntry
	if err := json.Unmarshal([]byte(messagesJSON), &entries); err != nil {
		return nil, metadata, fmt.Errorf("failed to unmarshal messages: %w", err)
	}

	return entries, metadata, nil
}

// loadConversationMetadata loads the metadata for a conversation
func (s *SQLiteStorage) loadConversationMetadata(ctx context.Context, conversationID string) (ConversationMetadata, string, error) {
	var metadata ConversationMetadata
	var messagesJSON, modelsJSON, tagsJSON, costStatsJSON string
	var totalInputTokens, totalOutputTokens, requestCount int
	var titleGenerationTime sql.NullTime

	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, count, messages, total_input_tokens, total_output_tokens,
		       request_count, cost_stats, models, tags, title_generated, title_invalidated, title_generation_time,
		       created_at, updated_at
		FROM conversations WHERE id = ?
	`, conversationID).Scan(
		&metadata.ID, &metadata.Title, &metadata.MessageCount,
		&messagesJSON, &totalInputTokens, &totalOutputTokens,
		&requestCount, &costStatsJSON, &modelsJSON, &tagsJSON,
		&metadata.TitleGenerated, &metadata.TitleInvalidated, &titleGenerationTime,
		&metadata.CreatedAt, &metadata.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return metadata, "", fmt.Errorf("conversation not found: %s", conversationID)
		}
		return metadata, "", fmt.Errorf("failed to load conversation: %w", err)
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

	return metadata, messagesJSON, nil
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
		       models, tags, title_generated, title_invalidated, title_generation_time
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
			&modelsJSON, &tagsJSON,
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
		SET title = ?, updated_at = ?, models = ?, tags = ?,
		    total_input_tokens = ?, total_output_tokens = ?, request_count = ?, cost_stats = ?,
		    title_generated = ?, title_invalidated = ?, title_generation_time = ?
		WHERE id = ?
	`, metadata.Title, metadata.UpdatedAt, modelsJSON, string(tagsJSON),
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

// GetSessionGroup returns the entry for groupKey or (_, false, nil) if missing.
func (s *SQLiteStorage) GetSessionGroup(ctx context.Context, groupKey string) (SessionGroupEntry, bool, error) {
	const query = `
		SELECT current_session_id, history, last_rollover, updated_at
		FROM session_groups
		WHERE group_key = ?
	`

	var (
		currentSessionID string
		historyJSON      string
		lastRollover     sql.NullTime
		updatedAt        time.Time
	)

	err := s.db.QueryRowContext(ctx, query, groupKey).Scan(&currentSessionID, &historyJSON, &lastRollover, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return SessionGroupEntry{}, false, nil
		}
		return SessionGroupEntry{}, false, fmt.Errorf("query session group %s: %w", groupKey, err)
	}

	var history []string
	if historyJSON != "" {
		if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
			return SessionGroupEntry{}, false, fmt.Errorf("decode history for %s: %w", groupKey, err)
		}
	}

	entry := SessionGroupEntry{
		CurrentSessionID: currentSessionID,
		History:          history,
		UpdatedAt:        updatedAt,
	}
	if lastRollover.Valid {
		entry.LastRollover = lastRollover.Time
	}
	return entry, true, nil
}

// PutSessionGroup creates or replaces the entry for groupKey via UPSERT.
func (s *SQLiteStorage) PutSessionGroup(ctx context.Context, groupKey string, entry SessionGroupEntry) error {
	historyJSON, err := json.Marshal(entry.History)
	if err != nil {
		return fmt.Errorf("encode history for %s: %w", groupKey, err)
	}
	if len(historyJSON) == 0 || string(historyJSON) == "null" {
		historyJSON = []byte("[]")
	}

	var lastRollover any
	if !entry.LastRollover.IsZero() {
		lastRollover = entry.LastRollover
	}

	const query = `
		INSERT INTO session_groups(group_key, current_session_id, history, last_rollover, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(group_key) DO UPDATE SET
			current_session_id = excluded.current_session_id,
			history            = excluded.history,
			last_rollover      = excluded.last_rollover,
			updated_at         = excluded.updated_at
	`

	if _, err := s.db.ExecContext(ctx, query, groupKey, entry.CurrentSessionID, string(historyJSON), lastRollover, entry.UpdatedAt); err != nil {
		return fmt.Errorf("upsert session group %s: %w", groupKey, err)
	}
	return nil
}

// ListSessionGroups returns all session-group entries.
func (s *SQLiteStorage) ListSessionGroups(ctx context.Context) (map[string]SessionGroupEntry, error) {
	const query = `
		SELECT group_key, current_session_id, history, last_rollover, updated_at
		FROM session_groups
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list session groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]SessionGroupEntry)
	for rows.Next() {
		var (
			groupKey         string
			currentSessionID string
			historyJSON      string
			lastRollover     sql.NullTime
			updatedAt        time.Time
		)
		if err := rows.Scan(&groupKey, &currentSessionID, &historyJSON, &lastRollover, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan session group row: %w", err)
		}

		var history []string
		if historyJSON != "" {
			if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
				return nil, fmt.Errorf("decode history for %s: %w", groupKey, err)
			}
		}

		entry := SessionGroupEntry{
			CurrentSessionID: currentSessionID,
			History:          history,
			UpdatedAt:        updatedAt,
		}
		if lastRollover.Valid {
			entry.LastRollover = lastRollover.Time
		}
		out[groupKey] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session groups: %w", err)
	}
	return out, nil
}
