package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestMCPConstants(t *testing.T) {
	if config.MCPFileName != "mcp.yaml" {
		t.Errorf("Expected MCPFileName to be 'mcp.yaml', got %q", config.MCPFileName)
	}

	expectedPath := config.ConfigDirName + "/" + config.MCPFileName
	if config.DefaultMCPPath != expectedPath {
		t.Errorf("Expected DefaultMCPPath to be %q, got %q", expectedPath, config.DefaultMCPPath)
	}
}

func TestDefaultMCPConfig(t *testing.T) {
	cfg := config.DefaultMCPConfig()

	if cfg == nil {
		t.Fatal("DefaultMCPConfig() returned nil")
	}

	if cfg.Enabled {
		t.Error("Expected Enabled to be false by default")
	}

	if cfg.ConnectionTimeout != 30 {
		t.Errorf("Expected ConnectionTimeout to be 30, got %d", cfg.ConnectionTimeout)
	}

	if cfg.DiscoveryTimeout != 30 {
		t.Errorf("Expected DiscoveryTimeout to be 30, got %d", cfg.DiscoveryTimeout)
	}

	if cfg.Servers == nil {
		t.Error("Expected Servers to be initialized")
	}

	if len(cfg.Servers) != 0 {
		t.Errorf("Expected Servers to be empty, got %d servers", len(cfg.Servers))
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
			name: "no include or exclude lists - include all",
			entry: config.MCPServerEntry{
				Name: "test-server",
			},
			toolName:      "any_tool",
			shouldInclude: true,
		},
		{
			name: "include list only - tool in list",
			entry: config.MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{"tool1", "tool2", "tool3"},
			},
			toolName:      "tool2",
			shouldInclude: true,
		},
		{
			name: "include list only - tool not in list",
			entry: config.MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{"tool1", "tool2", "tool3"},
			},
			toolName:      "tool4",
			shouldInclude: false,
		},
		{
			name: "exclude list only - tool in list",
			entry: config.MCPServerEntry{
				Name:         "test-server",
				ExcludeTools: []string{"dangerous_tool", "risky_tool"},
			},
			toolName:      "dangerous_tool",
			shouldInclude: false,
		},
		{
			name: "exclude list only - tool not in list",
			entry: config.MCPServerEntry{
				Name:         "test-server",
				ExcludeTools: []string{"dangerous_tool", "risky_tool"},
			},
			toolName:      "safe_tool",
			shouldInclude: true,
		},
		{
			name: "both lists - tool in include list",
			entry: config.MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{"tool1", "tool2"},
				ExcludeTools: []string{"tool2", "tool3"},
			},
			toolName:      "tool2",
			shouldInclude: true,
		},
		{
			name: "both lists - tool not in include list",
			entry: config.MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{"tool1", "tool2"},
				ExcludeTools: []string{"tool3"},
			},
			toolName:      "tool3",
			shouldInclude: false,
		},
		{
			name: "empty lists - include all",
			entry: config.MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{},
				ExcludeTools: []string{},
			},
			toolName:      "any_tool",
			shouldInclude: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.entry.ShouldIncludeTool(tt.toolName)
			if result != tt.shouldInclude {
				t.Errorf("ShouldIncludeTool(%q) = %v, want %v", tt.toolName, result, tt.shouldInclude)
			}
		})
	}
}

func TestMCPServerEntry_GetTimeout(t *testing.T) {
	tests := []struct {
		name           string
		serverTimeout  int
		globalTimeout  int
		expectedResult int
	}{
		{
			name:           "server timeout set - use server timeout",
			serverTimeout:  60,
			globalTimeout:  30,
			expectedResult: 60,
		},
		{
			name:           "server timeout not set - use global timeout",
			serverTimeout:  0,
			globalTimeout:  45,
			expectedResult: 45,
		},
		{
			name:           "neither set - use default",
			serverTimeout:  0,
			globalTimeout:  0,
			expectedResult: 30,
		},
		{
			name:           "server timeout zero but global set - use global",
			serverTimeout:  0,
			globalTimeout:  120,
			expectedResult: 120,
		},
		{
			name:           "both set - server takes precedence",
			serverTimeout:  90,
			globalTimeout:  30,
			expectedResult: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := config.MCPServerEntry{
				Timeout: tt.serverTimeout,
			}
			result := entry.GetTimeout(tt.globalTimeout)
			if result != tt.expectedResult {
				t.Errorf("GetTimeout(%d) = %d, want %d", tt.globalTimeout, result, tt.expectedResult)
			}
		})
	}
}

func TestLoadMCP(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		env   map[string]string
		check func(t *testing.T, cfg *config.MCPConfig)
	}{
		{
			name: "non-existent file returns defaults",
			check: func(t *testing.T, cfg *config.MCPConfig) {
				defaultCfg := config.DefaultMCPConfig()
				if cfg.Enabled != defaultCfg.Enabled {
					t.Errorf("Expected Enabled=%v, got %v", defaultCfg.Enabled, cfg.Enabled)
				}
			},
		},
		{
			name: "valid yaml",
			yaml: `enabled: true
connection_timeout: 60
discovery_timeout: 45
servers:
  - name: test-server
    scheme: http
    host: localhost
    ports:
      - "3000:8080"
    path: /sse
    enabled: true
    timeout: 30
    description: Test MCP server
    include_tools:
      - tool1
      - tool2
    exclude_tools:
      - tool3
`,
			check: func(t *testing.T, cfg *config.MCPConfig) {
				if !cfg.Enabled {
					t.Error("Expected Enabled to be true")
				}
				if cfg.ConnectionTimeout != 60 {
					t.Errorf("Expected ConnectionTimeout=60, got %d", cfg.ConnectionTimeout)
				}
				if cfg.DiscoveryTimeout != 45 {
					t.Errorf("Expected DiscoveryTimeout=45, got %d", cfg.DiscoveryTimeout)
				}
				if len(cfg.Servers) != 1 {
					t.Fatalf("Expected 1 server, got %d", len(cfg.Servers))
				}

				server := cfg.Servers[0]
				if server.Name != "test-server" {
					t.Errorf("Expected server name 'test-server', got %q", server.Name)
				}

				expectedURL := "http://localhost:3000/sse"
				if server.GetURL() != expectedURL {
					t.Errorf("Expected server URL %q, got %q", expectedURL, server.GetURL())
				}
				if !server.Enabled {
					t.Error("Expected server to be enabled")
				}
				if server.Timeout != 30 {
					t.Errorf("Expected server timeout=30, got %d", server.Timeout)
				}
				if len(server.IncludeTools) != 2 {
					t.Errorf("Expected 2 include tools, got %d", len(server.IncludeTools))
				}
				if len(server.ExcludeTools) != 1 {
					t.Errorf("Expected 1 exclude tool, got %d", len(server.ExcludeTools))
				}
			},
		},
		{
			name: "environment variable expansion",
			env:  map[string]string{"TEST_MCP_URL": "http://env-server:8080/sse"},
			yaml: `enabled: true
servers:
  - name: env-server
    scheme: http
    host: env-server
    ports:
      - "8080:8080"
    path: /sse
    enabled: true
`,
			check: func(t *testing.T, cfg *config.MCPConfig) {
				if len(cfg.Servers) != 1 {
					t.Fatalf("Expected 1 server, got %d", len(cfg.Servers))
				}

				server := cfg.Servers[0]
				expectedURL := "http://env-server:8080/sse"
				if server.GetURL() != expectedURL {
					t.Errorf("Expected URL %q, got %q", expectedURL, server.GetURL())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "mcp.yaml")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.yaml != "" {
				if err := os.WriteFile(configPath, []byte(tt.yaml), 0644); err != nil {
					t.Fatalf("Failed to write test config file: %v", err)
				}
			}

			cfg, err := config.LoadMCP(configPath)
			if err != nil {
				t.Fatalf("LoadMCP() failed: %v", err)
			}
			if cfg == nil {
				t.Fatal("LoadMCP() returned nil config")
			}
			tt.check(t, cfg)
		})
	}
}

func TestSaveMCP(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "subdir", "mcp.yaml")

	cfg := &config.MCPConfig{
		Enabled:           true,
		ConnectionTimeout: 60,
		DiscoveryTimeout:  45,
		Servers: []config.MCPServerEntry{
			{
				Name:        "test-server",
				Scheme:      "http",
				Host:        "localhost",
				Ports:       []string{"3000:8080"},
				Path:        "/sse",
				Enabled:     true,
				Timeout:     30,
				Description: "Test server",
			},
		},
	}

	if err := config.SaveMCP(configPath, cfg); err != nil {
		t.Fatalf("SaveMCP() failed: %v", err)
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	loadedCfg, err := config.LoadMCP(configPath)
	if err != nil {
		t.Fatalf("LoadMCP() after SaveMCP() failed: %v", err)
	}
	if loadedCfg.Enabled != cfg.Enabled {
		t.Errorf("Expected Enabled=%v, got %v", cfg.Enabled, loadedCfg.Enabled)
	}
	if len(loadedCfg.Servers) != len(cfg.Servers) {
		t.Errorf("Expected %d servers, got %d", len(cfg.Servers), len(loadedCfg.Servers))
	}
}

func loadMCPForTest(t *testing.T, path string) *config.MCPConfig {
	t.Helper()
	cfg, err := config.LoadMCP(path)
	if err != nil {
		t.Fatalf("LoadMCP() failed: %v", err)
	}
	return cfg
}

func seedMCPServers(t *testing.T, cfg *config.MCPConfig, servers []config.MCPServerEntry) {
	t.Helper()
	for _, s := range servers {
		if err := cfg.CreateEntry(s); err != nil {
			t.Fatalf("Failed to seed server %q: %v", s.Name, err)
		}
	}
}

func TestCreateEntry_MCPServer(t *testing.T) {
	server := config.MCPServerEntry{
		Name:        "new-server",
		Scheme:      "http",
		Host:        "localhost",
		Ports:       []string{"4000:8080"},
		Path:        "/sse",
		Enabled:     true,
		Description: "New server",
	}

	tests := []struct {
		name    string
		seed    []config.MCPServerEntry
		entry   config.MCPServerEntry
		wantErr bool
	}{
		{
			name:  "creates new server",
			entry: server,
		},
		{
			name:    "rejects duplicate name",
			seed:    []config.MCPServerEntry{server},
			entry:   server,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "mcp.yaml")
			cfg := loadMCPForTest(t, configPath)
			seedMCPServers(t, cfg, tt.seed)

			err := cfg.CreateEntry(tt.entry)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error when adding duplicate server, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateEntry() failed: %v", err)
			}

			reloaded := loadMCPForTest(t, configPath)
			if len(reloaded.Servers) != 1 {
				t.Fatalf("Expected 1 server, got %d", len(reloaded.Servers))
			}
			if reloaded.Servers[0].Name != tt.entry.Name {
				t.Errorf("Expected server name %q, got %q", tt.entry.Name, reloaded.Servers[0].Name)
			}
		})
	}
}

func TestUpdateEntry_MCPServer(t *testing.T) {
	initialServer := config.MCPServerEntry{
		Name:    "update-server",
		Scheme:  "http",
		Host:    "localhost",
		Ports:   []string{"3000:8080"},
		Path:    "/sse",
		Enabled: true,
	}

	tests := []struct {
		name    string
		seed    []config.MCPServerEntry
		entry   config.MCPServerEntry
		wantErr bool
	}{
		{
			name: "updates existing server",
			seed: []config.MCPServerEntry{initialServer},
			entry: config.MCPServerEntry{
				Name:        "update-server",
				Scheme:      "http",
				Host:        "localhost",
				Ports:       []string{"5000:8080"},
				Path:        "/sse",
				Enabled:     false,
				Description: "Updated description",
			},
		},
		{
			name: "not found returns error",
			entry: config.MCPServerEntry{
				Name:    "nonexistent-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"3000:8080"},
				Path:    "/sse",
				Enabled: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "mcp.yaml")
			cfg := loadMCPForTest(t, configPath)
			seedMCPServers(t, cfg, tt.seed)

			err := cfg.UpdateEntry(tt.entry)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error when updating non-existent server, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateEntry() failed: %v", err)
			}

			reloaded := loadMCPForTest(t, configPath)
			if len(reloaded.Servers) != 1 {
				t.Fatalf("Expected 1 server, got %d", len(reloaded.Servers))
			}

			server := reloaded.Servers[0]
			if server.GetURL() != tt.entry.GetURL() {
				t.Errorf("Expected URL %q, got %q", tt.entry.GetURL(), server.GetURL())
			}
			if server.Enabled != tt.entry.Enabled {
				t.Errorf("Expected Enabled=%v, got %v", tt.entry.Enabled, server.Enabled)
			}
			if server.Description != tt.entry.Description {
				t.Errorf("Expected Description %q, got %q", tt.entry.Description, server.Description)
			}
		})
	}
}

func TestDeleteEntry_MCPServer(t *testing.T) {
	server1 := config.MCPServerEntry{Name: "server1", Scheme: "http", Host: "localhost", Ports: []string{"3000:8080"}, Path: "/sse", Enabled: true}
	server2 := config.MCPServerEntry{Name: "server2", Scheme: "http", Host: "localhost", Ports: []string{"4000:8080"}, Path: "/sse", Enabled: true}

	tests := []struct {
		name          string
		seed          []config.MCPServerEntry
		remove        string
		wantErr       bool
		wantRemaining string
	}{
		{
			name:          "deletes existing server",
			seed:          []config.MCPServerEntry{server1, server2},
			remove:        "server1",
			wantRemaining: "server2",
		},
		{
			name:    "not found returns error",
			remove:  "nonexistent-server",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "mcp.yaml")
			cfg := loadMCPForTest(t, configPath)
			seedMCPServers(t, cfg, tt.seed)

			err := cfg.DeleteEntry(tt.remove)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error when removing non-existent server, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteEntry() failed: %v", err)
			}

			reloaded := loadMCPForTest(t, configPath)
			if len(reloaded.Servers) != 1 {
				t.Fatalf("Expected 1 server after removal, got %d", len(reloaded.Servers))
			}
			if reloaded.Servers[0].Name != tt.wantRemaining {
				t.Errorf("Expected remaining server to be %q, got %q", tt.wantRemaining, reloaded.Servers[0].Name)
			}
		})
	}
}

func TestListEntries_MCPServers(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	cfg := loadMCPForTest(t, configPath)
	if servers := cfg.ListEntries(); len(servers) != 0 {
		t.Errorf("Expected 0 servers initially, got %d", len(servers))
	}

	server1 := config.MCPServerEntry{Name: "server1", Scheme: "http", Host: "localhost", Ports: []string{"3000:8080"}, Path: "/sse", Enabled: true}
	server2 := config.MCPServerEntry{Name: "server2", Scheme: "http", Host: "localhost", Ports: []string{"4000:8080"}, Path: "/sse", Enabled: false}

	if err := cfg.CreateEntry(server1); err != nil {
		t.Fatalf("Failed to add server1: %v", err)
	}
	if err := cfg.CreateEntry(server2); err != nil {
		t.Fatalf("Failed to add server2: %v", err)
	}

	reloaded := loadMCPForTest(t, configPath)
	if servers := reloaded.ListEntries(); len(servers) != 2 {
		t.Fatalf("Expected 2 servers, got %d", len(servers))
	}
}

func TestReadEntry_MCPServer(t *testing.T) {
	expectedServer := config.MCPServerEntry{
		Name:        "get-server",
		Scheme:      "http",
		Host:        "localhost",
		Ports:       []string{"3000:8080"},
		Path:        "/sse",
		Enabled:     true,
		Description: "Test server",
	}

	tests := []struct {
		name    string
		seed    []config.MCPServerEntry
		read    string
		wantErr bool
	}{
		{
			name: "reads existing server",
			seed: []config.MCPServerEntry{expectedServer},
			read: "get-server",
		},
		{
			name:    "not found returns error",
			read:    "nonexistent-server",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "mcp.yaml")
			cfg := loadMCPForTest(t, configPath)
			seedMCPServers(t, cfg, tt.seed)

			server, err := cfg.ReadEntry(tt.read)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error when getting non-existent server, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadEntry() failed: %v", err)
			}
			if server.Name != expectedServer.Name {
				t.Errorf("Expected name %q, got %q", expectedServer.Name, server.Name)
			}
			if server.GetURL() != expectedServer.GetURL() {
				t.Errorf("Expected URL %q, got %q", expectedServer.GetURL(), server.GetURL())
			}
		})
	}
}
