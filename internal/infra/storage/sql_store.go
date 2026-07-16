package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// sqlStore is the shared SQL implementation of ConversationStorage and
// SessionGroupStorage. Both the SQLite and Postgres backends embed it: they
// speak the same single-table, embedded-messages schema (the one D1 also uses
// via GetSQLiteMigrations) and differ only in placeholder style, which rebind
// normalizes. See issue #839.
type sqlStore struct {
	db      *sql.DB
	dialect string // "sqlite" | "postgres"
}

// rebind converts the SQLite "?" placeholders the statements are written with
// into "$1, $2, …" for Postgres; every other dialect is left untouched.
//
// ponytail: naive ?→$N rewrite, assumes no "?" inside a SQL string literal —
// true for every statement in this file. Upgrade to sqlx.Rebind only if a
// literal "?" ever appears.
func (s *sqlStore) rebind(query string) string {
	if s.dialect != "postgres" {
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

// DB returns the underlying database connection.
func (s *sqlStore) DB() *sql.DB {
	return s.db
}

// SaveConversation saves a conversation with its entries using the single-table
// schema (messages are stored as an embedded JSON blob).
func (s *sqlStore) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
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

	costStatsJSON, err := json.Marshal(metadata.CostStats)
	if err != nil {
		return fmt.Errorf("failed to marshal cost stats: %w", err)
	}

	_, err = s.db.ExecContext(ctx, s.rebind(`
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
	`), conversationID, metadata.Title, len(entries), string(messagesJSON),
		metadata.TokenStats.TotalInputTokens, metadata.TokenStats.TotalOutputTokens, metadata.TokenStats.RequestCount,
		string(costStatsJSON), string(modelsJSON), string(tagsJSON), metadata.TitleGenerated, metadata.TitleInvalidated,
		metadata.TitleGenerationTime, metadata.CreatedAt.Format(time.RFC3339), metadata.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to save conversation: %w", err)
	}

	return nil
}

// LoadConversation loads a conversation by its ID.
func (s *sqlStore) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
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

// loadConversationMetadata loads the metadata plus the raw messages blob.
func (s *sqlStore) loadConversationMetadata(ctx context.Context, conversationID string) (ConversationMetadata, string, error) {
	var metadata ConversationMetadata
	var messagesJSON, modelsJSON, tagsJSON, costStatsJSON string
	var totalInputTokens, totalOutputTokens, requestCount int
	var titleGenerationTime sql.NullTime

	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id, title, count, messages, total_input_tokens, total_output_tokens,
		       request_count, cost_stats, models, tags, title_generated, title_invalidated, title_generation_time,
		       created_at, updated_at
		FROM conversations WHERE id = ?
	`), conversationID).Scan(
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

// ListConversations returns a list of conversation summaries.
func (s *sqlStore) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, title, created_at, updated_at, count, total_input_tokens, total_output_tokens, request_count, cost_stats
		FROM conversations
		ORDER BY updated_at DESC
		LIMIT ? OFFSET ?
	`), limit, offset)
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

// ListConversationsNeedingTitles returns conversations that need title generation.
func (s *sqlStore) ListConversationsNeedingTitles(ctx context.Context, limit int) ([]ConversationSummary, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, title, created_at, updated_at, count, total_input_tokens, total_output_tokens, request_count,
		       models, tags, title_generated, title_invalidated, title_generation_time
		FROM conversations
		WHERE (title_generated = FALSE OR title_invalidated = TRUE)
		  AND count >= 2
		ORDER BY updated_at DESC
		LIMIT ?
	`), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query conversations needing titles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []ConversationSummary
	for rows.Next() {
		var summary ConversationSummary
		var totalInputTokens, totalOutputTokens, requestCount int
		var modelsJSON, tagsJSON string
		var titleGenerationTime sql.NullTime

		err := rows.Scan(
			&summary.ID, &summary.Title, &summary.CreatedAt, &summary.UpdatedAt,
			&summary.MessageCount, &totalInputTokens, &totalOutputTokens, &requestCount,
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
			RequestCount:      requestCount,
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

// DeleteConversation removes a conversation by its ID.
func (s *sqlStore) DeleteConversation(ctx context.Context, conversationID string) error {
	result, err := s.db.ExecContext(ctx, s.rebind("DELETE FROM conversations WHERE id = ?"), conversationID)
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

// UpdateConversationMetadata updates metadata for a conversation.
func (s *sqlStore) UpdateConversationMetadata(ctx context.Context, conversationID string, metadata ConversationMetadata) error {
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
		if modelsData, err := json.Marshal([]string{metadata.Model}); err == nil {
			modelsJSON = string(modelsData)
		}
	}

	result, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE conversations
		SET title = ?, updated_at = ?, models = ?, tags = ?,
		    total_input_tokens = ?, total_output_tokens = ?, request_count = ?, cost_stats = ?,
		    title_generated = ?, title_invalidated = ?, title_generation_time = ?
		WHERE id = ?
	`), metadata.Title, metadata.UpdatedAt.Format(time.RFC3339), modelsJSON, string(tagsJSON),
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

// Close closes the database connection.
func (s *sqlStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Health checks if the database is reachable and functional.
func (s *sqlStore) Health(ctx context.Context) error {
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
func (s *sqlStore) GetSessionGroup(ctx context.Context, groupKey string) (SessionGroupEntry, bool, error) {
	var (
		currentSessionID string
		historyJSON      []byte
		lastRollover     sql.NullTime
		updatedAt        time.Time
	)

	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT current_session_id, history, last_rollover, updated_at
		FROM session_groups
		WHERE group_key = ?
	`), groupKey).Scan(&currentSessionID, &historyJSON, &lastRollover, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return SessionGroupEntry{}, false, nil
		}
		return SessionGroupEntry{}, false, fmt.Errorf("query session group %s: %w", groupKey, err)
	}

	history, err := decodeSessionHistory(historyJSON, groupKey)
	if err != nil {
		return SessionGroupEntry{}, false, err
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
func (s *sqlStore) PutSessionGroup(ctx context.Context, groupKey string, entry SessionGroupEntry) error {
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

	_, err = s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO session_groups(group_key, current_session_id, history, last_rollover, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(group_key) DO UPDATE SET
			current_session_id = excluded.current_session_id,
			history            = excluded.history,
			last_rollover      = excluded.last_rollover,
			updated_at         = excluded.updated_at
	`), groupKey, entry.CurrentSessionID, historyJSON, lastRollover, entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert session group %s: %w", groupKey, err)
	}
	return nil
}

// ListSessionGroups returns all session-group entries.
func (s *sqlStore) ListSessionGroups(ctx context.Context) (map[string]SessionGroupEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT group_key, current_session_id, history, last_rollover, updated_at
		FROM session_groups
	`)
	if err != nil {
		return nil, fmt.Errorf("list session groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]SessionGroupEntry)
	for rows.Next() {
		var (
			groupKey         string
			currentSessionID string
			historyJSON      []byte
			lastRollover     sql.NullTime
			updatedAt        time.Time
		)
		if err := rows.Scan(&groupKey, &currentSessionID, &historyJSON, &lastRollover, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan session group row: %w", err)
		}

		history, err := decodeSessionHistory(historyJSON, groupKey)
		if err != nil {
			return nil, err
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

// decodeSessionHistory unmarshals a stored history blob (TEXT in both dialects,
// scanned as []byte) into a slice of session IDs.
func decodeSessionHistory(historyJSON []byte, groupKey string) ([]string, error) {
	if len(historyJSON) == 0 {
		return nil, nil
	}
	var history []string
	if err := json.Unmarshal(historyJSON, &history); err != nil {
		return nil, fmt.Errorf("decode history for %s: %w", groupKey, err)
	}
	return history, nil
}

// ---------------------------------------------------------------------------
// ScheduledJobStorage (sqlStore)
// ---------------------------------------------------------------------------

// SaveJob creates or updates a scheduled job via UPSERT.
func (s *sqlStore) SaveJob(ctx context.Context, job *domain.ScheduledJob) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO scheduled_jobs(id, name, description, cron_expression, prompt, channel, recipient_id, model, run_once, created_at, updated_at, last_run, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			cron_expression = excluded.cron_expression,
			prompt = excluded.prompt,
			channel = excluded.channel,
			recipient_id = excluded.recipient_id,
			model = excluded.model,
			run_once = excluded.run_once,
			updated_at = excluded.updated_at,
			last_run = excluded.last_run,
			last_error = excluded.last_error
	`), job.ID, job.Name, job.Description, job.CronExpression, job.Prompt,
		job.Channel, job.RecipientID, job.Model, job.RunOnce,
		job.CreatedAt, job.UpdatedAt, job.LastRun, job.LastError)
	if err != nil {
		return fmt.Errorf("save scheduled job %s: %w", job.ID, err)
	}
	return nil
}

// LoadJob returns a job by ID. Returns ErrJobNotFound when the job does not exist.
func (s *sqlStore) LoadJob(ctx context.Context, id string) (*domain.ScheduledJob, error) {
	var job domain.ScheduledJob
	var lastRun sql.NullTime

	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id, name, description, cron_expression, prompt, channel, recipient_id, model, run_once, created_at, updated_at, last_run, last_error
		FROM scheduled_jobs WHERE id = ?
	`), id).Scan(
		&job.ID, &job.Name, &job.Description, &job.CronExpression, &job.Prompt,
		&job.Channel, &job.RecipientID, &job.Model, &job.RunOnce,
		&job.CreatedAt, &job.UpdatedAt, &lastRun, &job.LastError,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("load scheduled job %s: %w", id, err)
	}
	if lastRun.Valid {
		job.LastRun = &lastRun.Time
	}
	return &job, nil
}

// ListJobs returns all jobs sorted by CreatedAt ascending.
func (s *sqlStore) ListJobs(ctx context.Context) ([]*domain.ScheduledJob, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, name, description, cron_expression, prompt, channel, recipient_id, model, run_once, created_at, updated_at, last_run, last_error
		FROM scheduled_jobs ORDER BY created_at ASC
	`))
	if err != nil {
		return nil, fmt.Errorf("list scheduled jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []*domain.ScheduledJob
	for rows.Next() {
		var job domain.ScheduledJob
		var lastRun sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.Name, &job.Description, &job.CronExpression, &job.Prompt,
			&job.Channel, &job.RecipientID, &job.Model, &job.RunOnce,
			&job.CreatedAt, &job.UpdatedAt, &lastRun, &job.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan scheduled job: %w", err)
		}
		if lastRun.Valid {
			job.LastRun = &lastRun.Time
		}
		jobs = append(jobs, &job)
	}
	return jobs, rows.Err()
}

// DeleteJob removes a job by ID. Returns ErrJobNotFound when the job does not exist.
func (s *sqlStore) DeleteJob(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, s.rebind("DELETE FROM scheduled_jobs WHERE id = ?"), id)
	if err != nil {
		return fmt.Errorf("delete scheduled job %s: %w", id, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrJobNotFound
	}
	return nil
}

// Watch returns a channel that polls every 2s for changes via updated_at.
func (s *sqlStore) Watch(ctx context.Context) <-chan ScheduledJobChangeEvent {
	ch := make(chan ScheduledJobChangeEvent)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		// Track known job IDs and their updated_at timestamps
		known := make(map[string]time.Time)
		// Initial load
		jobs, err := s.ListJobs(ctx)
		if err == nil {
			for _, j := range jobs {
				known[j.ID] = j.UpdatedAt
			}
		}
		for {
			select {
			case <-ctx.Done():
				close(ch)
				return
			case <-ticker.C:
				current, err := s.ListJobs(ctx)
				if err != nil {
					continue
				}
				seen := make(map[string]bool)
				for _, j := range current {
					seen[j.ID] = true
					if prev, ok := known[j.ID]; !ok {
						ch <- ScheduledJobChangeEvent{ID: j.ID, Type: "create"}
					} else if !j.UpdatedAt.Equal(prev) {
						ch <- ScheduledJobChangeEvent{ID: j.ID, Type: "update"}
					}
					known[j.ID] = j.UpdatedAt
				}
				for id := range known {
					if !seen[id] {
						ch <- ScheduledJobChangeEvent{ID: id, Type: "delete"}
						delete(known, id)
					}
				}
			}
		}
	}()
	return ch
}

// ---------------------------------------------------------------------------
// PlanStorage (sqlStore)
// ---------------------------------------------------------------------------

// SavePlan creates a plan record via UPSERT.
func (s *sqlStore) SavePlan(ctx context.Context, plan *PlanRecord) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO plans(id, title, slug, body, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			slug = excluded.slug,
			body = excluded.body,
			created_at = excluded.created_at
	`), plan.ID, plan.Title, plan.Slug, plan.Body, plan.CreatedAt)
	if err != nil {
		return fmt.Errorf("save plan %s: %w", plan.ID, err)
	}
	return nil
}

// LoadPlan returns a plan by ID.
func (s *sqlStore) LoadPlan(ctx context.Context, id string) (*PlanRecord, error) {
	var plan PlanRecord
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id, title, slug, body, created_at FROM plans WHERE id = ?
	`), id).Scan(&plan.ID, &plan.Title, &plan.Slug, &plan.Body, &plan.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("plan not found: %s", id)
		}
		return nil, fmt.Errorf("load plan %s: %w", id, err)
	}
	return &plan, nil
}

// ListPlans returns all plans sorted by CreatedAt descending.
func (s *sqlStore) ListPlans(ctx context.Context) ([]*PlanRecord, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, title, slug, body, created_at FROM plans ORDER BY created_at DESC
	`))
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var plans []*PlanRecord
	for rows.Next() {
		var plan PlanRecord
		if err := rows.Scan(&plan.ID, &plan.Title, &plan.Slug, &plan.Body, &plan.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		plans = append(plans, &plan)
	}
	return plans, rows.Err()
}

// DeletePlan removes a plan by ID.
func (s *sqlStore) DeletePlan(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, s.rebind("DELETE FROM plans WHERE id = ?"), id)
	if err != nil {
		return fmt.Errorf("delete plan %s: %w", id, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("plan not found: %s", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ShellHistoryStorage (sqlStore)
// ---------------------------------------------------------------------------

// AppendHistory appends a command to the shell history log.
func (s *sqlStore) AppendHistory(ctx context.Context, command string) error {
	_, err := s.db.ExecContext(ctx, s.rebind("INSERT INTO shell_history(command) VALUES (?)"), command)
	if err != nil {
		return fmt.Errorf("append shell history: %w", err)
	}
	return nil
}

// LoadHistory returns the most recent commands up to limit.
func (s *sqlStore) LoadHistory(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT command FROM shell_history ORDER BY id DESC LIMIT ?
	`), limit)
	if err != nil {
		return nil, fmt.Errorf("load shell history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var commands []string
	for rows.Next() {
		var cmd string
		if err := rows.Scan(&cmd); err != nil {
			return nil, fmt.Errorf("scan shell history: %w", err)
		}
		commands = append(commands, cmd)
	}
	// Reverse to get chronological order
	for i, j := 0, len(commands)-1; i < j; i, j = i+1, j-1 {
		commands[i], commands[j] = commands[j], commands[i]
	}
	return commands, rows.Err()
}
