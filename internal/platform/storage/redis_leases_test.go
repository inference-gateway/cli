package storage

import (
	"net"
	"os"
	"strconv"
	"testing"

	require "github.com/stretchr/testify/require"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TestRedisStorage_LeaseConformance runs the lease suite against a real Redis
// server, which is what executes the claim script's Lua copy of Lease.Yields.
// It is skipped unless INFER_TEST_REDIS_ADDR (host:port) is set. CI wires it
// via a redis service container.
func TestRedisStorage_LeaseConformance(t *testing.T) {
	addr := os.Getenv("INFER_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set INFER_TEST_REDIS_ADDR to run the redis lease conformance suite")
	}
	host, portText, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	runLeaseStoreConformance(t, func(t *testing.T) agentdomain.LeaseStore {
		storage, err := NewRedisStorage(RedisConfig{Host: host, Port: port})
		require.NoError(t, err)
		t.Cleanup(func() { _ = storage.Close() })
		require.NoError(t, storage.client.FlushDB(t.Context()).Err())
		return storage
	})
}
