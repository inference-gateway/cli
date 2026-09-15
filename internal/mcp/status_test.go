package mcp

import (
	"context"
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
	if got := report.Indicator(); got != "🔌 0/2" {
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

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
		ok     bool
	}{
		{"ipv4 mapping", "3000/tcp -> 0.0.0.0:3001\n", 3001, true},
		{"ipv6 first", "3000/tcp -> [::]:3002\n3000/tcp -> 0.0.0.0:3002\n", 3002, true},
		{"no mapping", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseHostPort(tt.output)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseHostPort(%q) = %d, %v; want %d, %v", tt.output, got, ok, tt.want, tt.ok)
			}
		})
	}
}
