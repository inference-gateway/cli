package mcp

import (
	"context"
	"fmt"
	"sync"

	config "github.com/inference-gateway/cli/config"
)

// ServerStatus is the probe result for one configured MCP server.
type ServerStatus struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Tools     int    `json:"tools"`
	Error     string `json:"error,omitempty"`
}

// StatusReport mirrors the TUI's "🔌 connected/total (tools)" indicator plus
// a per-server breakdown. TotalServers counts every configured server, like
// the TUI; only enabled servers are dialed.
type StatusReport struct {
	Enabled          bool           `json:"enabled"`
	TotalServers     int            `json:"total_servers"`
	ConnectedServers int            `json:"connected_servers"`
	TotalTools       int            `json:"total_tools"`
	Servers          []ServerStatus `json:"servers"`
}

// Indicator renders the report the way the TUI status bar does.
func (r StatusReport) Indicator() string {
	if r.TotalTools > 0 {
		return fmt.Sprintf("🔌 %d/%d (%d)", r.ConnectedServers, r.TotalServers, r.TotalTools)
	}
	return fmt.Sprintf("🔌 %d/%d", r.ConnectedServers, r.TotalServers)
}

// ProbeStatus dials every enabled server once (or just `only` when non-empty)
// and reports what a fresh session would see. Connection state is per process,
// so a probe is the only honest answer from outside a running session.
func ProbeStatus(ctx context.Context, cfg *config.MCPConfig, only string) (StatusReport, error) {
	report := StatusReport{Enabled: cfg.Enabled, TotalServers: len(cfg.Servers)}

	servers := cfg.Servers
	if only != "" {
		servers = nil
		for _, server := range cfg.Servers {
			if server.Name == only {
				servers = []config.MCPServerEntry{server}
			}
		}
		if servers == nil {
			return report, fmt.Errorf("MCP server %q not found", only)
		}
	}

	report.Servers = make([]ServerStatus, len(servers))
	var wg sync.WaitGroup
	for i, server := range servers {
		wg.Add(1)
		go func(i int, server config.MCPServerEntry) {
			defer wg.Done()
			report.Servers[i] = probeServer(ctx, cfg, server)
		}(i, server)
	}
	wg.Wait()

	for _, server := range report.Servers {
		if server.Connected {
			report.ConnectedServers++
			report.TotalTools += server.Tools
		}
	}
	return report, nil
}

func probeServer(ctx context.Context, cfg *config.MCPConfig, server config.MCPServerEntry) ServerStatus {
	status := ServerStatus{Name: server.Name}
	if !server.Enabled {
		status.Error = "disabled"
		return status
	}

	client := newMCPClient(server, cfg)
	client.initializeClient(server.GetURL())
	discovered, err := client.DiscoverTools(ctx)
	if err != nil {
		status.Error = err.Error()
		return status
	}

	status.Connected = true
	status.Tools = len(discovered[server.Name])
	return status
}
