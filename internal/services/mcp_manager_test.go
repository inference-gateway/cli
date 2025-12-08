package services

import (
	"context"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestNewMCPManager(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled:           true,
		ConnectionTimeout: 30,
		DiscoveryTimeout:  30,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
		},
	}

	sessionID := domain.GenerateSessionID()
	manager := NewMCPManager(sessionID, cfg)

	if manager == nil {
		t.Fatal("Expected non-nil manager")
	}

	if manager.config != cfg {
		t.Error("Expected config to be set correctly")
	}

	clients := manager.GetClients()
	if len(clients) != 1 {
		t.Errorf("Expected 1 client, got %d", len(clients))
	}
}

func TestMCPManager_Close(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{},
	}

	sessionID := domain.GenerateSessionID()
	manager := NewMCPManager(sessionID, cfg)

	err := manager.Close()
	if err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
}

func TestMCPManager_GetClients_NoServers(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{},
	}

	sessionID := domain.GenerateSessionID()
	manager := NewMCPManager(sessionID, cfg)

	clients := manager.GetClients()

	if clients == nil {
		t.Fatal("Expected non-nil clients slice")
	}

	if len(clients) != 0 {
		t.Errorf("Expected 0 clients, got %d", len(clients))
	}
}

func TestMCPManager_GetClients_DisabledServer(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "disabled-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: false,
			},
		},
	}

	sessionID := domain.GenerateSessionID()
	manager := NewMCPManager(sessionID, cfg)

	clients := manager.GetClients()

	if len(clients) != 0 {
		t.Errorf("Expected 0 clients for disabled server, got %d", len(clients))
	}
}

func TestMCPManager_GetClients_MultipleServers(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "server1",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
			{
				Name:    "server2",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8081:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
			{
				Name:    "disabled-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8082:8080"},
				Path:    "/mcp",
				Enabled: false,
			},
		},
	}

	sessionID := domain.GenerateSessionID()
	manager := NewMCPManager(sessionID, cfg)

	clients := manager.GetClients()

	if len(clients) != 2 {
		t.Errorf("Expected 2 clients, got %d", len(clients))
	}
}

func TestMCPManager_StartMonitoring_Idempotent(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled:               true,
		LivenessProbeEnabled:  false,
		LivenessProbeInterval: 10,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
		},
	}

	sessionID := domain.GenerateSessionID()
	manager := NewMCPManager(sessionID, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chan1 := manager.StartMonitoring(ctx)
	chan2 := manager.StartMonitoring(ctx)

	if chan1 != chan2 {
		t.Error("StartMonitoring should be idempotent and return the same channel")
	}

	_ = manager.Close()
}

func TestMCPManager_StartMonitoring_DisabledProbes(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled:               true,
		LivenessProbeEnabled:  false,
		LivenessProbeInterval: 10,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
		},
	}

	sessionID := domain.GenerateSessionID()
	manager := NewMCPManager(sessionID, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	statusChan := manager.StartMonitoring(ctx)

	select {
	case _, ok := <-statusChan:
		if ok {
			t.Error("Expected channel to be closed when probes are disabled")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Channel should be closed immediately when probes are disabled")
	}

	_ = manager.Close()
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

// Ensure MCPManager implements domain.MCPManager interface
var _ domain.MCPManager = (*MCPManager)(nil)
