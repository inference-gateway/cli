package infrastructure

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// TestPostgresMessageQueue_Conformance runs the shared queue suite against a
// real PostgreSQL server. It is skipped unless INFER_TEST_POSTGRES_DSN is set
// (a space-separated keyword DSN, e.g.
// "host=localhost port=5432 user=postgres password=postgres dbname=infer_test sslmode=disable").
// CI wires the DSN via a postgres service container.
func TestPostgresMessageQueue_Conformance(t *testing.T) {
	dsn := os.Getenv("INFER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set INFER_TEST_POSTGRES_DSN to run the postgres queue conformance suite")
	}
	cfg := parseQueuedPostgresDSN(dsn)
	prefix := fmt.Sprintf("infer-%d", time.Now().UnixNano())

	newQueue := func(t *testing.T) convdomain.MessageQueue {
		return newScopedTestQueue(t, prefix, func(scope string) (convdomain.MessageQueue, error) {
			return NewPostgresMessageQueue(cfg, scope, time.Hour, func() {})
		})
	}

	runMessageQueueConformance(t, newQueue)
	runMessageQueueRestartPersistence(t, newQueue)
	runMessageQueueDrainRace(t, newQueue)
}

// parseQueuedPostgresDSN parses a space-separated "key=value" libpq DSN into a
// PostgresConfig. Values containing spaces are not supported - fine for test
// DSNs. Mirrors the storage package's own test parser.
func parseQueuedPostgresDSN(dsn string) storage.PostgresConfig {
	cfg := storage.PostgresConfig{Port: 5432, SSLMode: "disable"}
	for field := range strings.FieldsSeq(dsn) {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "host":
			cfg.Host = val
		case "port":
			cfg.Port, _ = strconv.Atoi(val)
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
