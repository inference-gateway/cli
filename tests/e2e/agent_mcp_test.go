//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	require "github.com/stretchr/testify/require"
)

// mcpServer is a stateless MCP server of protocol 2026-07-28 with one echo
// tool. It refuses the initialize handshake and sessions, and counts what it
// answers so a test tells a server nobody contacted from one an agent used.
type mcpServer struct {
	url      string
	requests atomic.Int64
	calls    atomic.Int64
}

func startMCPServer(t *testing.T, name string) *mcpServer {
	t.Helper()
	s := &mcpServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Method == "initialize" || r.Header.Get("Mcp-Session-Id") != "" {
			t.Errorf("%s: the client sent %s with a stateful handshake or a session", name, req.Method)
			http.Error(w, "stateless server", http.StatusBadRequest)
			return
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "server/discover":
			reply["result"] = map[string]any{"supportedVersions": []string{"2026-07-28"}}
		case "tools/list":
			reply["result"] = map[string]any{"tools": []any{map[string]any{
				"name":        "echo",
				"description": "Echoes the text back.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
			}}}
		case "tools/call":
			s.calls.Add(1)
			reply["result"] = map[string]any{"content": []any{map[string]any{
				"type": "text",
				"text": fmt.Sprintf("%s echoes %v", name, req.Params.Arguments["text"]),
			}}}
		default:
			reply["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL + "/mcp"
	return s
}

// writeMCPConfig gives home an mcp.yaml with chosen, which only an agent naming
// it brings up, and other, which every thread gets by default.
func writeMCPConfig(t *testing.T, home string, chosen, other *mcpServer) {
	t.Helper()
	dir := filepath.Join(home, ".infer")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	config := fmt.Sprintf(`enabled: true
servers:
  - name: chosen
    enabled: false
    url: %s
  - name: other
    enabled: true
    url: %s
`, chosen.url, other.url)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.yaml"), []byte(config), 0o600))
}

func TestDaemonConnectsOnlyTheMCPServersItsNamedAgentNames(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	chosen, other := startMCPServer(t, "chosen"), startMCPServer(t, "other")
	writeMCPConfig(t, home, chosen, other)
	writeUserAgent(t, home, ".infer", "e2e-mcp-agent", "mcpServers:\n  - chosen\n  - ghost\n", agentBody)
	m := startMock(t)
	d := startDaemon(t, home, "INFER_GATEWAY_MOCK=false", "INFER_GATEWAY_RUN=false", "INFER_GATEWAY_URL="+m.URL)

	agent := dialDaemon(t, d, "desktop")
	agent.send(map[string]any{"type": "new_session", "project_dir": project, "mode": "auto", "model": testModel, "agent": "e2e-mcp-agent"})
	agent.next("MESSAGES_SNAPSHOT")
	agent.say("echo ping through the agent's mcp server")
	require.Contains(t, replyText(agent.run()), "The agent's MCP server answered.")

	asked := requestsFor(m, "agent-mcp-echo")
	require.Len(t, asked, 2, "the tool call and the reply after its result")
	offered := advertisedTools(t, asked[0])
	require.Contains(t, offered, "MCP_chosen_echo", "the server the agent names comes up, although mcp.yaml disables it")
	require.NotContains(t, offered, "MCP_other_echo", "a server the agent does not name stays down")
	require.Contains(t, string(asked[1].RawBody), "chosen echoes ping", "the call reached the agent's server")
	require.Equal(t, int64(1), chosen.calls.Load())
	require.Zero(t, other.requests.Load(), "the agent's worker never contacts a server it does not name")

	plain := dialDaemon(t, d, "desktop")
	plain.send(map[string]any{"type": "new_session", "project_dir": t.TempDir(), "mode": "auto", "model": testModel})
	plain.next("MESSAGES_SNAPSHOT")
	plain.say("which model are you running on")
	plain.run()

	answered := requestsFor(m, "named-agent-own-model")
	require.Len(t, answered, 1)
	offered = advertisedTools(t, answered[0])
	require.Contains(t, offered, "MCP_other_echo", "a thread without an agent gets the enabled servers")
	require.NotContains(t, offered, "MCP_chosen_echo", "a disabled server stays down for a thread without an agent")
}
