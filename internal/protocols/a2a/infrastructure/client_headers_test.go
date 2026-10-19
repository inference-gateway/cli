package infrastructure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	uuid "github.com/google/uuid"

	adk "github.com/inference-gateway/adk/types"
)

// TestEntryHeadersReachRequestsExpanded sends an entry's agents.yaml headers
// with ${VARS} expanded on JSON-RPC calls to the agent.
func TestEntryHeadersReachRequestsExpanded(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  map[string]any{"id": "task-1", "status": map[string]any{"state": "submitted"}},
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	agentsYAML := filepath.Join(dir, ".infer", "agents.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(agentsYAML), 0o700))
	require.NoError(t, os.WriteFile(agentsYAML, []byte(
		"agents:\n"+
			"  - name: peer\n"+
			"    url: "+server.URL+"\n"+
			"    headers:\n"+
			"      Authorization: Bearer ${PEER_TOKEN}\n"+
			"      X-Custom-Trace: trace-plain\n"), 0o600))

	t.Setenv("PEER_TOKEN", "peer-secret")
	t.Chdir(dir)

	c := NewClient(server.URL)
	_, err := c.SendTask(context.Background(), adk.SendMessageRequest{
		Message: adk.Message{MessageID: uuid.NewString(), Role: adk.RoleUser, Parts: []adk.Part{adk.CreateTextPart("hi")}},
	})
	require.NoError(t, err)

	require.Equal(t, "Bearer peer-secret", got.Get("Authorization"), "expanded header")
	require.Equal(t, "trace-plain", got.Get("X-Custom-Trace"), "plain header")
}
