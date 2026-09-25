package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestProbeStatus(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled:           true,
		ConnectionTimeout: 1,
		Servers: []config.MCPServerEntry{
			{Name: "down", Host: "127.0.0.1", Port: 1, Path: "/mcp", Enabled: true},
			{Name: "off", Host: "127.0.0.1", Port: 1, Path: "/mcp", Enabled: false},
		},
	}

	report, err := ProbeStatus(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalServers != 2 || report.ConnectedServers != 0 || report.TotalTools != 0 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if report.Servers[0].Connected || report.Servers[0].Error == "" {
		t.Fatalf("unreachable server should report an error: %+v", report.Servers[0])
	}
	if report.Servers[1].Error != "disabled" {
		t.Fatalf("disabled server should not be dialed: %+v", report.Servers[1])
	}
	if got := report.Indicator(); got != "MCP 0/2" {
		t.Fatalf("indicator = %q", got)
	}

	only, err := ProbeStatus(context.Background(), cfg, "off")
	if err != nil || len(only.Servers) != 1 || only.Servers[0].Name != "off" {
		t.Fatalf("filter by name: %+v, %v", only, err)
	}
	if _, err := ProbeStatus(context.Background(), cfg, "nope"); err == nil {
		t.Fatal("unknown server name should error")
	}
}

// mcpServerEntry serves handle as an MCP server and returns its config entry.
func mcpServerEntry(t *testing.T, name string, handle func(method string) any) config.MCPServerEntry {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(handle(req.Method))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return config.MCPServerEntry{Name: name, Host: u.Hostname(), Port: port, Path: "/mcp", Enabled: true}
}

// A 2026-07-28 server reports its tools; a legacy server that has no
// server/discover is unavailable and told which protocol infer speaks.
func TestProbeStatus_ProtocolVersion(t *testing.T) {
	current := mcpServerEntry(t, "current", func(method string) any {
		switch method {
		case "server/discover":
			return map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"supportedVersions": []string{"2026-07-28"}}}
		default:
			return map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": []any{
				map[string]any{"name": "mcp_fs_read"}, map[string]any{"name": "mcp_fs_write"},
			}}}
		}
	})
	legacy := mcpServerEntry(t, "legacy", func(string) any {
		return map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32601, "message": "method not found"}}
	})
	cfg := &config.MCPConfig{Enabled: true, ConnectionTimeout: 5, Servers: []config.MCPServerEntry{current, legacy}}

	report, err := ProbeStatus(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Servers[0].Connected || report.Servers[0].Tools != 2 {
		t.Errorf("2026-07-28 server: %+v", report.Servers[0])
	}
	if report.Servers[1].Connected || !strings.Contains(report.Servers[1].Error, "must speak protocol version 2026-07-28") {
		t.Errorf("legacy server: %+v", report.Servers[1])
	}
}
