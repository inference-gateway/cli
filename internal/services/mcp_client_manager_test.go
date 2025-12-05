package services

import (
	"context"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestNewMCPClientManager(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled:           true,
		ConnectionTimeout: 30,
		DiscoveryTimeout:  30,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				URL:     "http://localhost:8080/mcp",
				Enabled: true,
			},
		},
	}

	manager := NewMCPClientManager(cfg)

	if manager == nil {
		t.Fatal("Expected non-nil manager")
	}

	if manager.config != cfg {
		t.Error("Expected config to be set correctly")
	}
}

func TestMCPClientManager_Close(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{},
	}

	manager := NewMCPClientManager(cfg)

	// Close should not error even if stateless
	err := manager.Close()
	if err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
}

func TestMCPClientManager_DiscoverTools_NoServers(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled:          true,
		DiscoveryTimeout: 5,
		Servers:          []config.MCPServerEntry{},
	}

	manager := NewMCPClientManager(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := manager.DiscoverTools(ctx)

	if err != nil {
		t.Errorf("DiscoverTools() returned error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if len(result) != 0 {
		t.Errorf("Expected empty result, got %d servers", len(result))
	}
}

func TestMCPClientManager_DiscoverTools_DisabledServer(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled:          true,
		DiscoveryTimeout: 5,
		Servers: []config.MCPServerEntry{
			{
				Name:    "disabled-server",
				URL:     "http://localhost:8080/mcp",
				Enabled: false,
			},
		},
	}

	manager := NewMCPClientManager(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := manager.DiscoverTools(ctx)

	if err != nil {
		t.Errorf("DiscoverTools() returned error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if len(result) != 0 {
		t.Errorf("Expected empty result for disabled server, got %d servers", len(result))
	}
}

func TestMCPClientManager_CallTool_ServerNotFound(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "server1",
				URL:     "http://localhost:8080/mcp",
				Enabled: true,
			},
		},
	}

	manager := NewMCPClientManager(cfg)

	ctx := context.Background()

	_, err := manager.CallTool(ctx, "nonexistent-server", "someTool", map[string]any{})

	if err == nil {
		t.Error("Expected error for non-existent server")
	}

	expectedMsg := "not found in configuration"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got: %v", expectedMsg, err)
	}
}

func TestMCPClientManager_CallTool_DisabledServer(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "disabled-server",
				URL:     "http://localhost:8080/mcp",
				Enabled: false,
			},
		},
	}

	manager := NewMCPClientManager(cfg)

	ctx := context.Background()

	_, err := manager.CallTool(ctx, "disabled-server", "someTool", map[string]any{})

	if err == nil {
		t.Error("Expected error for disabled server")
	}

	expectedMsg := "is disabled"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got: %v", expectedMsg, err)
	}
}

func TestMCPServerEntry_ShouldIncludeTool(t *testing.T) {
	tests := []struct {
		name          string
		entry         config.MCPServerEntry
		toolName      string
		shouldInclude bool
	}{
		{
			name: "no filters - include all",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{},
				ExcludeTools: []string{},
			},
			toolName:      "anyTool",
			shouldInclude: true,
		},
		{
			name: "include filter - tool in list",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{"readFile", "writeFile"},
				ExcludeTools: []string{},
			},
			toolName:      "readFile",
			shouldInclude: true,
		},
		{
			name: "include filter - tool not in list",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{"readFile", "writeFile"},
				ExcludeTools: []string{},
			},
			toolName:      "deleteTool",
			shouldInclude: false,
		},
		{
			name: "exclude filter - tool in list",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{},
				ExcludeTools: []string{"dangerousTool", "deleteTool"},
			},
			toolName:      "dangerousTool",
			shouldInclude: false,
		},
		{
			name: "exclude filter - tool not in list",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{},
				ExcludeTools: []string{"dangerousTool", "deleteTool"},
			},
			toolName:      "safeTool",
			shouldInclude: true,
		},
		{
			name: "both filters - include takes precedence",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{"readFile", "writeFile"},
				ExcludeTools: []string{"writeFile"},
			},
			toolName:      "writeFile",
			shouldInclude: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.entry.ShouldIncludeTool(tt.toolName)
			if result != tt.shouldInclude {
				t.Errorf("ShouldIncludeTool(%s) = %v, expected %v", tt.toolName, result, tt.shouldInclude)
			}
		})
	}
}

func TestMCPServerEntry_GetTimeout(t *testing.T) {
	tests := []struct {
		name           string
		serverTimeout  int
		defaultTimeout int
		expected       int
	}{
		{
			name:           "use server timeout when set",
			serverTimeout:  60,
			defaultTimeout: 30,
			expected:       60,
		},
		{
			name:           "use default timeout when server timeout is 0",
			serverTimeout:  0,
			defaultTimeout: 30,
			expected:       30,
		},
		{
			name:           "use server timeout even if smaller than default",
			serverTimeout:  10,
			defaultTimeout: 30,
			expected:       10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := config.MCPServerEntry{
				Timeout: tt.serverTimeout,
			}

			result := entry.GetTimeout(tt.defaultTimeout)
			if result != tt.expected {
				t.Errorf("GetTimeout() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

// Ensure MCPClientManager implements domain.MCPClient interface
var _ domain.MCPClient = (*MCPClientManager)(nil)
