package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newD1MockServer stands up an httptest.Server that speaks the D1 /query
// envelope, backed by a real in-process SQLite database. Because the backing
// store is genuine SQLite, round-trips exercise real SQL semantics (ORDER BY,
// changes==0 not-found, the title predicate, JSON round-trips) rather than
// hand-rolled fixtures.
func newD1MockServer(t *testing.T) *httptest.Server {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "d1mock.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleD1Query(t, db, w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// handleD1Query executes one /query request against the backing SQLite DB and
// renders the response in D1's envelope shape.
func handleD1Query(t *testing.T, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	var body d1RequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeD1Error(w, http.StatusBadRequest, "bad request body")
		return
	}

	if isD1Mutation(body.SQL) {
		res, err := db.ExecContext(r.Context(), body.SQL, body.Params...)
		if err != nil {
			writeD1Error(w, http.StatusOK, err.Error())
			return
		}
		changes, _ := res.RowsAffected()
		writeD1Result(w, nil, changes)
		return
	}

	rows, err := db.QueryContext(r.Context(), body.SQL, body.Params...)
	if err != nil {
		writeD1Error(w, http.StatusOK, err.Error())
		return
	}
	defer func() { _ = rows.Close() }()

	results, err := scanD1Rows(rows)
	if err != nil {
		writeD1Error(w, http.StatusOK, err.Error())
		return
	}
	writeD1Result(w, results, 0)
}

func isD1Mutation(sqlText string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(sqlText))
	for _, p := range []string{"INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER"} {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

// scanD1Rows generically scans rows into JSON objects, converting []byte→string
// and int64→float64 to mimic the types Cloudflare's JSON response carries
// (TEXT→string, INTEGER→number).
func scanD1Rows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		m := make(map[string]any, len(cols))
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			case int64:
				m[c] = float64(v)
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func writeD1Result(w http.ResponseWriter, results []map[string]any, changes int64) {
	if results == nil {
		results = []map[string]any{}
	}
	env := d1Envelope{
		Success: true,
		Result: []d1QueryResult{{
			Results: results,
			Success: true,
			Meta:    d1Meta{Changes: changes},
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(env)
}

func writeD1Error(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(d1Envelope{
		Success: false,
		Errors:  []d1APIError{{Message: msg}},
	})
}

func setupTestD1Storage(t *testing.T) *D1Storage {
	srv := newD1MockServer(t)
	storage, err := NewD1Storage(D1Config{
		AccountID:  "test-acct",
		DatabaseID: "test-db",
		APIToken:   "test-token",
		BaseURL:    srv.URL,
	})
	require.NoError(t, err)
	return storage
}

func TestD1Storage_BasicOperations(t *testing.T) {
	storage := setupTestD1Storage(t)
	ctx := context.Background()

	t.Run("Health Check", func(t *testing.T) {
		assert.NoError(t, storage.Health(ctx))
	})

	t.Run("Save and Load Conversation", func(t *testing.T) {
		conversationID := "test-conversation-1"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		require.NoError(t, err)

		assert.Equal(t, metadata.ID, loadedMetadata.ID)
		assert.Equal(t, metadata.Title, loadedMetadata.Title)
		assert.Equal(t, len(entries), loadedMetadata.MessageCount)
		assert.Equal(t, metadata.TokenStats, loadedMetadata.TokenStats)
		assert.Equal(t, metadata.Tags, loadedMetadata.Tags)

		assert.Len(t, loadedEntries, len(entries))
		for i, entry := range entries {
			assert.Equal(t, entry.Message.Content, loadedEntries[i].Message.Content)
			assert.Equal(t, entry.Message.Role, loadedEntries[i].Message.Role)
			assert.Equal(t, entry.Model, loadedEntries[i].Model)
			assert.Equal(t, entry.Hidden, loadedEntries[i].Hidden)
		}
	})

	t.Run("Update Conversation", func(t *testing.T) {
		conversationID := "test-conversation-update"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		newEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("Updated response"),
			},
			Time:  time.Now(),
			Model: "claude-4",
		}
		entries = append(entries, newEntry)

		metadata.Title = "Updated Title"
		metadata.UpdatedAt = time.Now()
		metadata.MessageCount = len(entries)

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		loadedEntries, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		require.NoError(t, err)

		assert.Equal(t, "Updated Title", loadedMetadata.Title)
		assert.Len(t, loadedEntries, len(entries))
		lastContent, _ := loadedEntries[len(loadedEntries)-1].Message.Content.AsMessageContent0()
		assert.Equal(t, "Updated response", lastContent)
	})
}

func TestD1Storage_ConversationManagement(t *testing.T) {
	storage := setupTestD1Storage(t)
	ctx := context.Background()

	t.Run("List Conversations", func(t *testing.T) {
		conversations := []string{"conv1", "conv2", "conv3"}

		for i, id := range conversations {
			entries := createTestEntries()
			metadata := createTestMetadata(id)
			metadata.Title = "Conversation " + string(rune('A'+i))
			metadata.CreatedAt = time.Now().Add(time.Duration(i) * time.Hour)
			metadata.UpdatedAt = metadata.CreatedAt

			require.NoError(t, storage.SaveConversation(ctx, id, entries, metadata))
		}

		summaries, err := storage.ListConversations(ctx, 10, 0)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(summaries), 3)

		for i := 1; i < len(summaries); i++ {
			assert.True(t, summaries[i-1].UpdatedAt.After(summaries[i].UpdatedAt) ||
				summaries[i-1].UpdatedAt.Equal(summaries[i].UpdatedAt))
		}
	})

	t.Run("Delete Conversation", func(t *testing.T) {
		conversationID := "test-conversation-delete"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		_, _, err := storage.LoadConversation(ctx, conversationID)
		require.NoError(t, err)

		require.NoError(t, storage.DeleteConversation(ctx, conversationID))

		_, _, err = storage.LoadConversation(ctx, conversationID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})

	t.Run("Update Metadata", func(t *testing.T) {
		conversationID := "test-conversation-metadata"
		entries := createTestEntries()
		metadata := createTestMetadata(conversationID)

		require.NoError(t, storage.SaveConversation(ctx, conversationID, entries, metadata))

		metadata.Title = "New Title"
		metadata.Tags = []string{"updated", "test"}
		metadata.UpdatedAt = time.Now()

		require.NoError(t, storage.UpdateConversationMetadata(ctx, conversationID, metadata))

		_, loadedMetadata, err := storage.LoadConversation(ctx, conversationID)
		require.NoError(t, err)

		assert.Equal(t, "New Title", loadedMetadata.Title)
		assert.Equal(t, []string{"updated", "test"}, loadedMetadata.Tags)
	})
}

func TestD1Storage_ErrorCases(t *testing.T) {
	storage := setupTestD1Storage(t)
	ctx := context.Background()

	t.Run("Load Non-existent Conversation", func(t *testing.T) {
		_, _, err := storage.LoadConversation(ctx, "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})

	t.Run("Delete Non-existent Conversation", func(t *testing.T) {
		err := storage.DeleteConversation(ctx, "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation not found")
	})
}

// TestD1Storage_ListConversationsNeedingTitles guards the issue's explicit
// warning: the title-generation batch path must receive Model/Tags/RequestCount
// (i.e. the full mapper, not the lean ListConversations one).
func TestD1Storage_ListConversationsNeedingTitles(t *testing.T) {
	storage := setupTestD1Storage(t)
	ctx := context.Background()

	for i := range 3 {
		id := fmt.Sprintf("needs-title-%d", i)
		entries := createTestEntries() // 4 entries → count >= 2
		metadata := createTestMetadata(id)
		metadata.TitleGenerated = false
		metadata.UpdatedAt = time.Now().Add(time.Duration(i) * time.Hour)
		require.NoError(t, storage.SaveConversation(ctx, id, entries, metadata))
	}

	summaries, err := storage.ListConversationsNeedingTitles(ctx, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(summaries), 3)

	for _, s := range summaries {
		assert.Equal(t, "claude-4", s.Model, "Model must be carried for the title batch path")
		assert.Equal(t, []string{"test", "demo"}, s.Tags, "Tags must be carried for the title batch path")
		assert.Equal(t, s.MessageCount/2, s.TokenStats.RequestCount)
		assert.False(t, s.TitleGenerated)
	}
}

func TestD1Storage_SessionGroups(t *testing.T) {
	s := setupTestD1Storage(t)
	ctx := context.Background()

	_, ok, err := s.GetSessionGroup(ctx, "missing")
	require.NoError(t, err)
	assert.False(t, ok, "missing key must report not-found")

	now := time.Now().UTC().Truncate(time.Second)
	entry := SessionGroupEntry{
		CurrentSessionID: "uuid-1",
		History:          []string{"prev-a", "prev-b"},
		LastRollover:     now,
		UpdatedAt:        now,
	}
	require.NoError(t, s.PutSessionGroup(ctx, "channel-telegram-42", entry))

	got, ok, err := s.GetSessionGroup(ctx, "channel-telegram-42")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "uuid-1", got.CurrentSessionID)
	assert.Equal(t, []string{"prev-a", "prev-b"}, got.History)
	assert.WithinDuration(t, now, got.UpdatedAt, time.Second)
	assert.WithinDuration(t, now, got.LastRollover, time.Second)

	require.NoError(t, s.PutSessionGroup(ctx, "channel-telegram-42", SessionGroupEntry{
		CurrentSessionID: "uuid-2",
		History:          []string{"prev-a", "prev-b", "uuid-1"},
		UpdatedAt:        now,
	}))

	got, ok, err = s.GetSessionGroup(ctx, "channel-telegram-42")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "uuid-2", got.CurrentSessionID)
	assert.Equal(t, []string{"prev-a", "prev-b", "uuid-1"}, got.History)
	assert.True(t, got.LastRollover.IsZero(), "LastRollover must be cleared when UPSERT supplies zero value")

	require.NoError(t, s.PutSessionGroup(ctx, "second", SessionGroupEntry{
		CurrentSessionID: "uuid-3",
		UpdatedAt:        now,
	}))
	all, err := s.ListSessionGroups(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)
	assert.Equal(t, "uuid-2", all["channel-telegram-42"].CurrentSessionID)
	assert.Equal(t, "uuid-3", all["second"].CurrentSessionID)
	assert.True(t, all["second"].LastRollover.IsZero())
}

// TestD1Storage_RequestShape asserts the driver hits the documented D1 endpoint
// path and sends the bearer token.
func TestD1Storage_RequestShape(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		writeD1Result(w, []map[string]any{{"ok": float64(1)}}, 0)
	}))
	defer srv.Close()

	storage, err := NewD1Storage(D1Config{
		AccountID:  "acct123",
		DatabaseID: "db456",
		APIToken:   "tok789",
		BaseURL:    srv.URL,
	})
	require.NoError(t, err)
	require.NoError(t, storage.Health(context.Background()))

	assert.Equal(t, "/accounts/acct123/d1/database/db456/query", gotPath)
	assert.Equal(t, "Bearer tok789", gotAuth)
	assert.Equal(t, "application/json", gotContentType)
}

// TestD1Storage_Live exercises a real Cloudflare D1 database. It is skipped
// unless INFER_TEST_D1_LIVE is set and the D1 credentials are provided.
func TestD1Storage_Live(t *testing.T) {
	if os.Getenv("INFER_TEST_D1_LIVE") == "" {
		t.Skip("set INFER_TEST_D1_LIVE=1 and INFER_STORAGE_D1_* to run the live D1 test")
	}

	cfg := D1Config{
		AccountID:  os.Getenv("INFER_STORAGE_D1_ACCOUNT_ID"),
		DatabaseID: os.Getenv("INFER_STORAGE_D1_DATABASE_ID"),
		APIToken:   os.Getenv("INFER_STORAGE_D1_API_TOKEN"),
		BaseURL:    os.Getenv("INFER_STORAGE_D1_BASE_URL"),
	}
	require.NotEmpty(t, cfg.AccountID)
	require.NotEmpty(t, cfg.DatabaseID)
	require.NotEmpty(t, cfg.APIToken)

	storage, err := NewD1Storage(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	id := "live-test-" + time.Now().UTC().Format("20060102150405")
	require.NoError(t, storage.SaveConversation(ctx, id, createTestEntries(), createTestMetadata(id)))
	t.Cleanup(func() { _ = storage.DeleteConversation(context.Background(), id) })

	_, md, err := storage.LoadConversation(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, id, md.ID)
	require.NoError(t, storage.Health(ctx))
}
