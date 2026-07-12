package storage

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"
)

// TestPostgresStorage_Conformance runs the shared storage suite against a real
// PostgreSQL server. It is skipped unless INFER_TEST_POSTGRES_DSN is set (a
// space-separated keyword DSN, e.g.
// "host=localhost port=5432 user=postgres password=postgres dbname=infer_test sslmode=disable").
//
// This is the postgres backend's first happy-path coverage — the bug it now
// exercises (list queries referencing a non-existent "summary" column) shipped
// undetected because postgres had no test (see issue #839). CI wires this via a
// postgres service container.
func TestPostgresStorage_Conformance(t *testing.T) {
	dsn := os.Getenv("INFER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set INFER_TEST_POSTGRES_DSN to run the postgres conformance suite")
	}

	cfg := parsePostgresDSN(t, dsn)

	runConversationStorageConformance(t, func(t *testing.T) ConversationStorage {
		storage, err := NewPostgresStorage(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = storage.Close() })

		// Isolate each conformance group: the shared server persists rows across
		// groups (and runs), so start every one from a clean slate.
		_, err = storage.DB().ExecContext(context.Background(), "TRUNCATE conversations, session_groups")
		require.NoError(t, err)

		return storage
	})
}

// parsePostgresDSN parses a space-separated "key=value" libpq DSN into a
// PostgresConfig. Values containing spaces are not supported — fine for test
// DSNs.
func parsePostgresDSN(t *testing.T, dsn string) PostgresConfig {
	t.Helper()

	cfg := PostgresConfig{Port: 5432, SSLMode: "disable"}
	for field := range strings.FieldsSeq(dsn) {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "host":
			cfg.Host = val
		case "port":
			port, err := strconv.Atoi(val)
			require.NoError(t, err, "invalid port in INFER_TEST_POSTGRES_DSN")
			cfg.Port = port
		case "user":
			cfg.Username = val
		case "password":
			cfg.Password = val
		case "dbname":
			cfg.Database = val
		case "sslmode":
			cfg.SSLMode = val
		}
	}
	return cfg
}
