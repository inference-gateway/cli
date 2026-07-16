package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	migrations "github.com/inference-gateway/cli/internal/infra/storage/migrations"
)

// D1Storage implements ConversationStorage and SessionGroupStorage on top of
// Cloudflare D1. D1 is SQLite exposed over an HTTP query API, so this driver
// issues the exact same SQL as SQLiteStorage but ships it over the network via
// POST /accounts/{account}/d1/database/{database}/query instead of a local
// file handle. Timestamps are stored as UTC RFC3339 strings so ORDER BY sorts
// chronologically regardless of the runner's timezone and external readers get
// unambiguous ISO-8601 values.
type D1Storage struct {
	accountID  string
	databaseID string
	apiToken   string
	baseURL    string
	httpClient *http.Client
}

const (
	d1DefaultBaseURL = "https://api.cloudflare.com/client/v4"
	d1HTTPTimeout    = 30 * time.Second
)

var (
	_ ConversationStorage = (*D1Storage)(nil)
	_ SessionGroupStorage = (*D1Storage)(nil)
)

// d1RequestBody is the JSON body of a D1 /query request.
type d1RequestBody struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}

// d1Meta carries per-statement execution metadata; we only need the affected
// row count to detect not-found on DELETE/UPDATE.
type d1Meta struct {
	Changes int64 `json:"changes"`
}

// d1QueryResult is one statement's result within the envelope.
type d1QueryResult struct {
	Results []map[string]any `json:"results"`
	Success bool             `json:"success"`
	Meta    d1Meta           `json:"meta"`
}

// d1APIError is a single error from the D1 API envelope.
type d1APIError struct {
	Message string `json:"message"`
}

// d1Envelope is the top-level Cloudflare API response wrapper.
type d1Envelope struct {
	Success bool            `json:"success"`
	Errors  []d1APIError    `json:"errors"`
	Result  []d1QueryResult `json:"result"`
}

// NewD1Storage creates a new Cloudflare D1 storage instance and ensures the
// schema exists (idempotent CREATE ... IF NOT EXISTS, byte-for-byte identical
// to the SQLite migrations).
func NewD1Storage(config D1Config) (*D1Storage, error) {
	if config.AccountID == "" || config.DatabaseID == "" || config.APIToken == "" {
		return nil, fmt.Errorf("d1 storage requires account_id, database_id, and api_token")
	}

	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = d1DefaultBaseURL
	}

	s := &D1Storage{
		accountID:  config.AccountID,
		databaseID: config.DatabaseID,
		apiToken:   config.APIToken,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: d1HTTPTimeout},
	}

	ctx, cancel := context.WithTimeout(context.Background(), d1HTTPTimeout)
	defer cancel()
	if err := s.runMigrations(ctx); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return s, nil
}

// runMigrations applies the SQLite schema over HTTP. The migration SQL is
// reused verbatim from the SQLite migrations to guarantee schema parity; each
// statement is sent individually so it works whether or not D1 accepts
// multi-statement queries.
func (s *D1Storage) runMigrations(ctx context.Context) error {
	for _, m := range migrations.GetSQLiteMigrations() {
		for _, stmt := range splitSQLStatements(m.UpSQL) {
			if _, err := s.exec(ctx, stmt); err != nil {
				return fmt.Errorf("migration %s (%s) failed: %w", m.Version, m.Description, err)
			}
		}
	}
	return nil
}

// splitSQLStatements breaks a migration block into individual statements. The
// migration DDL contains no semicolons inside string literals, so a plain split
// is safe.
func splitSQLStatements(sqlText string) []string {
	parts := strings.Split(sqlText, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// do posts a single SQL statement to the D1 /query endpoint and returns the
// first (and only) result set. D1 returns HTTP 200 with success=false for SQL
// errors, so the envelope is inspected even on a 2xx response.
func (s *D1Storage) do(ctx context.Context, query string, params []any) (*d1QueryResult, error) {
	if params == nil {
		params = []any{}
	}

	payload, err := json.Marshal(d1RequestBody{SQL: query, Params: params})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal d1 request: %w", err)
	}

	url := fmt.Sprintf("%s/accounts/%s/d1/database/%s/query", s.baseURL, s.accountID, s.databaseID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create d1 request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("d1 request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var env d1Envelope
	decodeErr := json.NewDecoder(resp.Body).Decode(&env)
	if resp.StatusCode != http.StatusOK {
		if decodeErr == nil && len(env.Errors) > 0 {
			return nil, fmt.Errorf("d1 API error (status %d): %s", resp.StatusCode, env.Errors[0].Message)
		}
		return nil, fmt.Errorf("d1 API returned status %d", resp.StatusCode)
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("failed to decode d1 response: %w", decodeErr)
	}
	if !env.Success {
		if len(env.Errors) > 0 {
			return nil, fmt.Errorf("d1 query failed: %s", env.Errors[0].Message)
		}
		return nil, fmt.Errorf("d1 query failed")
	}
	if len(env.Result) == 0 {
		return nil, fmt.Errorf("d1 returned no result set")
	}
	return &env.Result[0], nil
}

// exec runs a mutating statement and returns the number of affected rows.
func (s *D1Storage) exec(ctx context.Context, query string, args ...any) (int64, error) {
	res, err := s.do(ctx, query, d1Params(args...))
	if err != nil {
		return 0, err
	}
	return res.Meta.Changes, nil
}

// queryRows runs a SELECT and returns the result rows as decoded JSON objects.
func (s *D1Storage) queryRows(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	res, err := s.do(ctx, query, d1Params(args...))
	if err != nil {
		return nil, err
	}
	return res.Results, nil
}

// d1Params converts Go bind values into the JSON-safe scalars D1 accepts.
func d1Params(args ...any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = d1Param(a)
	}
	return out
}

// d1Param maps a single bind value: bools become 0/1 (matching SQLite BOOLEAN
// storage and the `= FALSE`/`= TRUE` predicates), times become UTC RFC3339
// strings (zero/nil → SQL NULL), and everything else passes through.
func d1Param(a any) any {
	switch v := a.(type) {
	case bool:
		if v {
			return 1
		}
		return 0
	case time.Time:
		if v.IsZero() {
			return nil
		}
		return v.UTC().Format(time.RFC3339)
	case *time.Time:
		if v == nil || v.IsZero() {
			return nil
		}
		return v.UTC().Format(time.RFC3339)
	default:
		return v
	}
}

// asString reads a TEXT/NULL column value as a string.
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asInt reads an INTEGER column value (decoded from JSON as float64).
func asInt(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// asBool reads a BOOLEAN column value stored as 0/1.
func asBool(v any) bool {
	if f, ok := v.(float64); ok {
		return f != 0
	}
	return false
}

// asTime parses a DATETIME column written by this driver (UTC RFC3339).
func asTime(v any) time.Time {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// asTimePtr returns nil for a NULL/zero DATETIME, otherwise a pointer to the parsed time.
func asTimePtr(v any) *time.Time {
	t := asTime(v)
	if t.IsZero() {
		return nil
	}
	return &t
}

// SaveConversation saves a conversation with its entries using the simplified schema.
func (s *D1Storage) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
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

	_, err = s.exec(ctx, `
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
	`, conversationID, metadata.Title, len(entries), string(messagesJSON), metadata.TokenStats.TotalInputTokens, metadata.TokenStats.TotalOutputTokens,
		metadata.TokenStats.RequestCount, string(costStatsJSON), string(modelsJSON), string(tagsJSON), metadata.TitleGenerated, metadata.TitleInvalidated,
		metadata.TitleGenerationTime, metadata.CreatedAt, metadata.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to save conversation: %w", err)
	}

	return nil
}

// LoadConversation loads a conversation by its ID using the simplified schema.
func (s *D1Storage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
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

// loadConversationMetadata loads the full metadata for a conversation.
func (s *D1Storage) loadConversationMetadata(ctx context.Context, conversationID string) (ConversationMetadata, string, error) {
	var metadata ConversationMetadata

	rows, err := s.queryRows(ctx, `
		SELECT id, title, count, messages, total_input_tokens, total_output_tokens,
		       request_count, cost_stats, models, tags, title_generated, title_invalidated, title_generation_time,
		       created_at, updated_at
		FROM conversations WHERE id = ?
	`, conversationID)
	if err != nil {
		return metadata, "", fmt.Errorf("failed to load conversation: %w", err)
	}
	if len(rows) == 0 {
		return metadata, "", fmt.Errorf("conversation not found: %s", conversationID)
	}
	r := rows[0]

	metadata.ID = asString(r["id"])
	metadata.Title = asString(r["title"])
	metadata.MessageCount = asInt(r["count"])
	metadata.TitleGenerated = asBool(r["title_generated"])
	metadata.TitleInvalidated = asBool(r["title_invalidated"])
	metadata.TitleGenerationTime = asTimePtr(r["title_generation_time"])
	metadata.CreatedAt = asTime(r["created_at"])
	metadata.UpdatedAt = asTime(r["updated_at"])

	totalInputTokens := asInt(r["total_input_tokens"])
	totalOutputTokens := asInt(r["total_output_tokens"])
	metadata.TokenStats = domain.SessionTokenStats{
		TotalInputTokens:  totalInputTokens,
		TotalOutputTokens: totalOutputTokens,
		TotalTokens:       totalInputTokens + totalOutputTokens,
		RequestCount:      asInt(r["request_count"]),
	}

	costStatsJSON := asString(r["cost_stats"])
	if costStatsJSON != "" && costStatsJSON != "{}" {
		if err := json.Unmarshal([]byte(costStatsJSON), &metadata.CostStats); err != nil {
			metadata.CostStats = domain.SessionCostStats{}
		}
	}

	modelsJSON := asString(r["models"])
	if modelsJSON != "" && modelsJSON != "[]" {
		var models []string
		if err := json.Unmarshal([]byte(modelsJSON), &models); err == nil && len(models) > 0 {
			metadata.Model = models[0]
		}
	}

	tagsJSON := asString(r["tags"])
	if tagsJSON != "" && tagsJSON != "[]" {
		_ = json.Unmarshal([]byte(tagsJSON), &metadata.Tags)
	}

	return metadata, asString(r["messages"]), nil
}

// ListConversations returns a list of conversation summaries (lean: no models/tags/title fields).
func (s *D1Storage) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	rows, err := s.queryRows(ctx, `
		SELECT id, title, created_at, updated_at, count, total_input_tokens, total_output_tokens, request_count, cost_stats
		FROM conversations
		ORDER BY updated_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query conversations: %w", err)
	}

	var summaries []ConversationSummary
	for _, r := range rows {
		var summary ConversationSummary
		summary.ID = asString(r["id"])
		summary.Title = asString(r["title"])
		summary.CreatedAt = asTime(r["created_at"])
		summary.UpdatedAt = asTime(r["updated_at"])
		summary.MessageCount = asInt(r["count"])

		totalInputTokens := asInt(r["total_input_tokens"])
		totalOutputTokens := asInt(r["total_output_tokens"])
		summary.TokenStats = domain.SessionTokenStats{
			TotalInputTokens:  totalInputTokens,
			TotalOutputTokens: totalOutputTokens,
			TotalTokens:       totalInputTokens + totalOutputTokens,
			RequestCount:      asInt(r["request_count"]),
		}

		costStatsJSON := asString(r["cost_stats"])
		if costStatsJSON != "" && costStatsJSON != "{}" {
			if err := json.Unmarshal([]byte(costStatsJSON), &summary.CostStats); err != nil {
				summary.CostStats = domain.SessionCostStats{}
			}
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

// ListConversationsNeedingTitles returns conversations that need title generation.
// It carries Model/Tags/TitleGenerated/TitleInvalidated/TitleGenerationTime (the
// title-generation batch path needs them) and therefore uses a dedicated mapper
// rather than the lean ListConversations one.
func (s *D1Storage) ListConversationsNeedingTitles(ctx context.Context, limit int) ([]ConversationSummary, error) {
	rows, err := s.queryRows(ctx, `
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

	var summaries []ConversationSummary
	for _, r := range rows {
		summaries = append(summaries, mapTitleSummaryRow(r))
	}

	return summaries, nil
}

// mapTitleSummaryRow maps a title-generation summary row, carrying the full
// model/tags/title field set.
func mapTitleSummaryRow(r map[string]any) ConversationSummary {
	var summary ConversationSummary
	summary.ID = asString(r["id"])
	summary.Title = asString(r["title"])
	summary.CreatedAt = asTime(r["created_at"])
	summary.UpdatedAt = asTime(r["updated_at"])
	summary.MessageCount = asInt(r["count"])
	summary.TitleGenerated = asBool(r["title_generated"])
	summary.TitleInvalidated = asBool(r["title_invalidated"])
	summary.TitleGenerationTime = asTimePtr(r["title_generation_time"])

	totalInputTokens := asInt(r["total_input_tokens"])
	totalOutputTokens := asInt(r["total_output_tokens"])
	summary.TokenStats = domain.SessionTokenStats{
		TotalInputTokens:  totalInputTokens,
		TotalOutputTokens: totalOutputTokens,
		TotalTokens:       totalInputTokens + totalOutputTokens,
		RequestCount:      summary.MessageCount / 2,
	}

	modelsJSON := asString(r["models"])
	if modelsJSON != "" && modelsJSON != "[]" {
		var models []string
		if err := json.Unmarshal([]byte(modelsJSON), &models); err == nil && len(models) > 0 {
			summary.Model = models[0]
		}
	}

	tagsJSON := asString(r["tags"])
	if tagsJSON != "" && tagsJSON != "[]" {
		_ = json.Unmarshal([]byte(tagsJSON), &summary.Tags)
	}

	return summary
}

// DeleteConversation removes a conversation by its ID.
func (s *D1Storage) DeleteConversation(ctx context.Context, conversationID string) error {
	changes, err := s.exec(ctx, "DELETE FROM conversations WHERE id = ?", conversationID)
	if err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}
	if changes == 0 {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}
	return nil
}

// UpdateConversationMetadata updates metadata for a conversation.
func (s *D1Storage) UpdateConversationMetadata(ctx context.Context, conversationID string, metadata ConversationMetadata) error {
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

	changes, err := s.exec(ctx, `
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
	if changes == 0 {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}
	return nil
}

// Close releases resources. D1 holds no persistent connection, so this is a no-op.
func (s *D1Storage) Close() error {
	return nil
}

// Health checks that the D1 database is reachable and answering queries.
func (s *D1Storage) Health(ctx context.Context) error {
	rows, err := s.queryRows(ctx, "SELECT 1 AS ok")
	if err != nil {
		return fmt.Errorf("d1 health check failed: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("d1 health check returned no rows")
	}
	return nil
}

// GetSessionGroup returns the entry for groupKey or (_, false, nil) if missing.
func (s *D1Storage) GetSessionGroup(ctx context.Context, groupKey string) (SessionGroupEntry, bool, error) {
	rows, err := s.queryRows(ctx, `
		SELECT current_session_id, history, last_rollover, updated_at
		FROM session_groups
		WHERE group_key = ?
	`, groupKey)
	if err != nil {
		return SessionGroupEntry{}, false, fmt.Errorf("query session group %s: %w", groupKey, err)
	}
	if len(rows) == 0 {
		return SessionGroupEntry{}, false, nil
	}
	r := rows[0]

	history, err := decodeHistory(asString(r["history"]), groupKey)
	if err != nil {
		return SessionGroupEntry{}, false, err
	}

	entry := SessionGroupEntry{
		CurrentSessionID: asString(r["current_session_id"]),
		History:          history,
		UpdatedAt:        asTime(r["updated_at"]),
	}
	if lr := asTimePtr(r["last_rollover"]); lr != nil {
		entry.LastRollover = *lr
	}
	return entry, true, nil
}

// PutSessionGroup creates or replaces the entry for groupKey via UPSERT.
func (s *D1Storage) PutSessionGroup(ctx context.Context, groupKey string, entry SessionGroupEntry) error {
	historyJSON, err := json.Marshal(entry.History)
	if err != nil {
		return fmt.Errorf("encode history for %s: %w", groupKey, err)
	}
	if len(historyJSON) == 0 || string(historyJSON) == "null" {
		historyJSON = []byte("[]")
	}

	_, err = s.exec(ctx, `
		INSERT INTO session_groups(group_key, current_session_id, history, last_rollover, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(group_key) DO UPDATE SET
			current_session_id = excluded.current_session_id,
			history            = excluded.history,
			last_rollover      = excluded.last_rollover,
			updated_at         = excluded.updated_at
	`, groupKey, entry.CurrentSessionID, string(historyJSON), entry.LastRollover, entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert session group %s: %w", groupKey, err)
	}
	return nil
}

// ListSessionGroups returns all session-group entries.
func (s *D1Storage) ListSessionGroups(ctx context.Context) (map[string]SessionGroupEntry, error) {
	rows, err := s.queryRows(ctx, `
		SELECT group_key, current_session_id, history, last_rollover, updated_at
		FROM session_groups
	`)
	if err != nil {
		return nil, fmt.Errorf("list session groups: %w", err)
	}

	out := make(map[string]SessionGroupEntry)
	for _, r := range rows {
		groupKey := asString(r["group_key"])
		history, err := decodeHistory(asString(r["history"]), groupKey)
		if err != nil {
			return nil, err
		}

		entry := SessionGroupEntry{
			CurrentSessionID: asString(r["current_session_id"]),
			History:          history,
			UpdatedAt:        asTime(r["updated_at"]),
		}
		if lr := asTimePtr(r["last_rollover"]); lr != nil {
			entry.LastRollover = *lr
		}
		out[groupKey] = entry
	}
	return out, nil
}

// decodeHistory unmarshals the session-group history JSON array.
func decodeHistory(historyJSON, groupKey string) ([]string, error) {
	if historyJSON == "" {
		return nil, nil
	}
	var history []string
	if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
		return nil, fmt.Errorf("decode history for %s: %w", groupKey, err)
	}
	return history, nil
}

// ---------------------------------------------------------------------------
// ScheduledJobStorage (D1Storage)
// ---------------------------------------------------------------------------

// SaveJob creates or updates a scheduled job via UPSERT.
func (s *D1Storage) SaveJob(ctx context.Context, job *domain.ScheduledJob) error {
_, err := s.exec(ctx, `
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
`, job.ID, job.Name, job.Description, job.CronExpression, job.Prompt,
	job.Channel, job.RecipientID, job.Model, job.RunOnce,
	job.CreatedAt, job.UpdatedAt, job.LastRun, job.LastError)
if err != nil {
	return fmt.Errorf("save scheduled job %s: %w", job.ID, err)
}
return nil
}

// LoadJob returns a job by ID.
func (s *D1Storage) LoadJob(ctx context.Context, id string) (*domain.ScheduledJob, error) {
rows, err := s.queryRows(ctx, `
	SELECT id, name, description, cron_expression, prompt, channel, recipient_id, model, run_once, created_at, updated_at, last_run, last_error
	FROM scheduled_jobs WHERE id = ?
`, id)
if err != nil {
	return nil, fmt.Errorf("load scheduled job %s: %w", id, err)
}
if len(rows) == 0 {
	return nil, ErrJobNotFound
}
r := rows[0]
job := &domain.ScheduledJob{
	ID:             asString(r["id"]),
	Name:           asString(r["name"]),
	Description:    asString(r["description"]),
	CronExpression: asString(r["cron_expression"]),
	Prompt:         asString(r["prompt"]),
	Channel:        asString(r["channel"]),
	RecipientID:    asString(r["recipient_id"]),
	Model:          asString(r["model"]),
	RunOnce:        asBool(r["run_once"]),
	CreatedAt:      asTime(r["created_at"]),
	UpdatedAt:      asTime(r["updated_at"]),
	LastError:      asString(r["last_error"]),
}
if lr := asTimePtr(r["last_run"]); lr != nil {
	job.LastRun = lr
}
return job, nil
}

// ListJobs returns all jobs sorted by CreatedAt ascending.
func (s *D1Storage) ListJobs(ctx context.Context) ([]*domain.ScheduledJob, error) {
rows, err := s.queryRows(ctx, `
	SELECT id, name, description, cron_expression, prompt, channel, recipient_id, model, run_once, created_at, updated_at, last_run, last_error
	FROM scheduled_jobs ORDER BY created_at ASC
`)
if err != nil {
	return nil, fmt.Errorf("list scheduled jobs: %w", err)
}
var jobs []*domain.ScheduledJob
for _, r := range rows {
	job := &domain.ScheduledJob{
		ID:             asString(r["id"]),
		Name:           asString(r["name"]),
		Description:    asString(r["description"]),
		CronExpression: asString(r["cron_expression"]),
		Prompt:         asString(r["prompt"]),
		Channel:        asString(r["channel"]),
		RecipientID:    asString(r["recipient_id"]),
		Model:          asString(r["model"]),
		RunOnce:        asBool(r["run_once"]),
		CreatedAt:      asTime(r["created_at"]),
		UpdatedAt:      asTime(r["updated_at"]),
		LastError:      asString(r["last_error"]),
	}
	if lr := asTimePtr(r["last_run"]); lr != nil {
		job.LastRun = lr
	}
	jobs = append(jobs, job)
}
return jobs, nil
}

// DeleteJob removes a job by ID.
func (s *D1Storage) DeleteJob(ctx context.Context, id string) error {
changes, err := s.exec(ctx, "DELETE FROM scheduled_jobs WHERE id = ?", id)
if err != nil {
	return fmt.Errorf("delete scheduled job %s: %w", id, err)
}
if changes == 0 {
	return ErrJobNotFound
}
return nil
}

// Watch returns a channel that polls every 2s for changes.
func (s *D1Storage) Watch(ctx context.Context) <-chan ScheduledJobChangeEvent {
ch := make(chan ScheduledJobChangeEvent)
go func() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	known := make(map[string]time.Time)
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
// PlanStorage (D1Storage)
// ---------------------------------------------------------------------------

// SavePlan creates a plan record via UPSERT.
func (s *D1Storage) SavePlan(ctx context.Context, plan *PlanRecord) error {
_, err := s.exec(ctx, `
	INSERT INTO plans(id, title, slug, body, created_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title = excluded.title,
		slug = excluded.slug,
		body = excluded.body,
		created_at = excluded.created_at
`, plan.ID, plan.Title, plan.Slug, plan.Body, plan.CreatedAt)
if err != nil {
	return fmt.Errorf("save plan %s: %w", plan.ID, err)
}
return nil
}

// LoadPlan returns a plan by ID.
func (s *D1Storage) LoadPlan(ctx context.Context, id string) (*PlanRecord, error) {
rows, err := s.queryRows(ctx, "SELECT id, title, slug, body, created_at FROM plans WHERE id = ?", id)
if err != nil {
	return nil, fmt.Errorf("load plan %s: %w", id, err)
}
if len(rows) == 0 {
	return nil, fmt.Errorf("plan not found: %s", id)
}
r := rows[0]
return &PlanRecord{
	ID:        asString(r["id"]),
	Title:     asString(r["title"]),
	Slug:      asString(r["slug"]),
	Body:      asString(r["body"]),
	CreatedAt: asTime(r["created_at"]),
}, nil
}

// ListPlans returns all plans sorted by CreatedAt descending.
func (s *D1Storage) ListPlans(ctx context.Context) ([]*PlanRecord, error) {
rows, err := s.queryRows(ctx, "SELECT id, title, slug, body, created_at FROM plans ORDER BY created_at DESC")
if err != nil {
	return nil, fmt.Errorf("list plans: %w", err)
}
var plans []*PlanRecord
for _, r := range rows {
	plans = append(plans, &PlanRecord{
		ID:        asString(r["id"]),
		Title:     asString(r["title"]),
		Slug:      asString(r["slug"]),
		Body:      asString(r["body"]),
		CreatedAt: asTime(r["created_at"]),
	})
}
return plans, nil
}

// DeletePlan removes a plan by ID.
func (s *D1Storage) DeletePlan(ctx context.Context, id string) error {
changes, err := s.exec(ctx, "DELETE FROM plans WHERE id = ?", id)
if err != nil {
	return fmt.Errorf("delete plan %s: %w", id, err)
}
if changes == 0 {
	return fmt.Errorf("plan not found: %s", id)
}
return nil
}

// ---------------------------------------------------------------------------
// ShellHistoryStorage (D1Storage)
// ---------------------------------------------------------------------------

// AppendHistory appends a command to the shell history.
func (s *D1Storage) AppendHistory(ctx context.Context, command string) error {
_, err := s.exec(ctx, "INSERT INTO shell_history(command) VALUES (?)", command)
if err != nil {
	return fmt.Errorf("append shell history: %w", err)
}
return nil
}

// LoadHistory returns the most recent commands up to limit.
func (s *D1Storage) LoadHistory(ctx context.Context, limit int) ([]string, error) {
rows, err := s.queryRows(ctx, "SELECT command FROM shell_history ORDER BY id DESC LIMIT ?", limit)
if err != nil {
	return nil, fmt.Errorf("load shell history: %w", err)
}
var commands []string
for _, r := range rows {
	commands = append(commands, asString(r["command"]))
}
// Reverse to get chronological order
for i, j := 0, len(commands)-1; i < j; i, j = i+1, j-1 {
	commands[i], commands[j] = commands[j], commands[i]
}
return commands, nil
}
