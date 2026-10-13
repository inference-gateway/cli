package infrastructure

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// TestRedisMessageQueue_Conformance runs the shared queue suite against a real
// Redis server. It is skipped unless INFER_TEST_REDIS_ADDR is set (e.g.
// "localhost:6379").
func TestRedisMessageQueue_Conformance(t *testing.T) {
	addr := os.Getenv("INFER_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set INFER_TEST_REDIS_ADDR (host:port) to run the redis queue conformance suite")
	}
	cfg, err := parseQueuedRedisAddr(addr)
	require.NoError(t, err)
	prefix := fmt.Sprintf("infer-%d", time.Now().UnixNano())

	newQueue := func(t *testing.T) convdomain.MessageQueue {
		return newScopedTestQueue(t, prefix, func(scope string) (convdomain.MessageQueue, error) {
			return NewRedisMessageQueue(cfg, scope, time.Hour, func() {})
		})
	}

	runMessageQueueConformance(t, newQueue)
	runMessageQueueRestartPersistence(t, newQueue)
	runMessageQueueDrainRace(t, newQueue)
}

// parseQueuedRedisAddr splits an "host:port" address into a RedisConfig.
func parseQueuedRedisAddr(addr string) (storage.RedisConfig, error) {
	host, portText, ok := strings.Cut(addr, ":")
	if !ok {
		return storage.RedisConfig{}, fmt.Errorf("invalid Redis address %q: want host:port", addr)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return storage.RedisConfig{}, fmt.Errorf("invalid Redis address %q: %w", addr, err)
	}
	return storage.RedisConfig{Host: host, Port: port}, nil
}
