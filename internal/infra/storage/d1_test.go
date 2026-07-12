package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestD1Storage_Conformance runs the shared storage suite against the D1 driver,
// backed by the httptest mock over real SQLite (unconditional in CI). D1 uses
// the same single-table schema as SQLite/Postgres (see #839).
func TestD1Storage_Conformance(t *testing.T) {
	runConversationStorageConformance(t, func(t *testing.T) ConversationStorage {
		return setupTestD1Storage(t)
	})
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
