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

func TestLoadMCP_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "non-existent.yaml")

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		t.Fatalf("LoadMCP() should not error for non-existent file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadMCP() returned nil config")
	}

	defaultCfg := config.DefaultMCPConfig()
	if cfg.Enabled != defaultCfg.Enabled {
		t.Errorf("Expected Enabled=%v, got %v", defaultCfg.Enabled, cfg.Enabled)
	}
}

func TestLoadMCP_ValidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	yamlContent := `enabled: true
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
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		t.Fatalf("LoadMCP() failed: %v", err)
	}

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
}

func TestLoadMCP_EnvironmentVariableExpansion(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	t.Setenv("TEST_MCP_URL", "http://env-server:8080/sse")

	yamlContent := `enabled: true
servers:
  - name: env-server
    scheme: http
    host: env-server
    ports:
      - "8080:8080"
    path: /sse
    enabled: true
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		t.Fatalf("LoadMCP() failed: %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(cfg.Servers))
	}

	server := cfg.Servers[0]
	expectedURL := "http://env-server:8080/sse"
	if server.GetURL() != expectedURL {
		t.Errorf("Expected URL %q, got %q", expectedURL, server.GetURL())
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

func TestCreateEntry_MCPServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	newServer := config.MCPServerEntry{
		Name:        "new-server",
		Scheme:      "http",
		Host:        "localhost",
		Ports:       []string{"4000:8080"},
		Path:        "/sse",
		Enabled:     true,
		Description: "New server",
	}

	cfg := loadMCPForTest(t, configPath)
	if err := cfg.CreateEntry(newServer); err != nil {
		t.Fatalf("CreateEntry() failed: %v", err)
	}

	reloaded := loadMCPForTest(t, configPath)
	if len(reloaded.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(reloaded.Servers))
	}
	if reloaded.Servers[0].Name != newServer.Name {
		t.Errorf("Expected server name %q, got %q", newServer.Name, reloaded.Servers[0].Name)
	}
}

func TestCreateEntry_MCPServer_DuplicateName(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	server := config.MCPServerEntry{
		Name:    "duplicate-server",
		Scheme:  "http",
		Host:    "localhost",
		Ports:   []string{"3000:8080"},
		Path:    "/sse",
		Enabled: true,
	}

	cfg := loadMCPForTest(t, configPath)
	if err := cfg.CreateEntry(server); err != nil {
		t.Fatalf("First CreateEntry() failed: %v", err)
	}
	if err := cfg.CreateEntry(server); err == nil {
		t.Fatal("Expected error when adding duplicate server, got nil")
	}
}

func TestUpdateEntry_MCPServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	initialServer := config.MCPServerEntry{
		Name:    "update-server",
		Scheme:  "http",
		Host:    "localhost",
		Ports:   []string{"3000:8080"},
		Path:    "/sse",
		Enabled: true,
	}
	cfg := loadMCPForTest(t, configPath)
	if err := cfg.CreateEntry(initialServer); err != nil {
		t.Fatalf("CreateEntry() failed: %v", err)
	}

	updatedServer := config.MCPServerEntry{
		Name:        "update-server",
		Scheme:      "http",
		Host:        "localhost",
		Ports:       []string{"5000:8080"},
		Path:        "/sse",
		Enabled:     false,
		Description: "Updated description",
	}
	if err := cfg.UpdateEntry(updatedServer); err != nil {
		t.Fatalf("UpdateEntry() failed: %v", err)
	}

	reloaded := loadMCPForTest(t, configPath)
	if len(reloaded.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(reloaded.Servers))
	}

	server := reloaded.Servers[0]
	if server.GetURL() != updatedServer.GetURL() {
		t.Errorf("Expected URL %q, got %q", updatedServer.GetURL(), server.GetURL())
	}
	if server.Enabled != updatedServer.Enabled {
		t.Errorf("Expected Enabled=%v, got %v", updatedServer.Enabled, server.Enabled)
	}
	if server.Description != updatedServer.Description {
		t.Errorf("Expected Description %q, got %q", updatedServer.Description, server.Description)
	}
}

func TestUpdateEntry_MCPServer_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	server := config.MCPServerEntry{
		Name:    "nonexistent-server",
		Scheme:  "http",
		Host:    "localhost",
		Ports:   []string{"3000:8080"},
		Path:    "/sse",
		Enabled: true,
	}
	cfg := loadMCPForTest(t, configPath)
	if err := cfg.UpdateEntry(server); err == nil {
		t.Fatal("Expected error when updating non-existent server, got nil")
	}
}

func TestDeleteEntry_MCPServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	server1 := config.MCPServerEntry{Name: "server1", Scheme: "http", Host: "localhost", Ports: []string{"3000:8080"}, Path: "/sse", Enabled: true}
	server2 := config.MCPServerEntry{Name: "server2", Scheme: "http", Host: "localhost", Ports: []string{"4000:8080"}, Path: "/sse", Enabled: true}

	cfg := loadMCPForTest(t, configPath)
	if err := cfg.CreateEntry(server1); err != nil {
		t.Fatalf("Failed to add server1: %v", err)
	}
	if err := cfg.CreateEntry(server2); err != nil {
		t.Fatalf("Failed to add server2: %v", err)
	}

	if err := cfg.DeleteEntry("server1"); err != nil {
		t.Fatalf("DeleteEntry() failed: %v", err)
	}

	reloaded := loadMCPForTest(t, configPath)
	if len(reloaded.Servers) != 1 {
		t.Fatalf("Expected 1 server after removal, got %d", len(reloaded.Servers))
	}
	if reloaded.Servers[0].Name != "server2" {
		t.Errorf("Expected remaining server to be 'server2', got %q", reloaded.Servers[0].Name)
	}
}

func TestDeleteEntry_MCPServer_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	cfg := loadMCPForTest(t, configPath)
	if err := cfg.DeleteEntry("nonexistent-server"); err == nil {
		t.Fatal("Expected error when removing non-existent server, got nil")
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
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	expectedServer := config.MCPServerEntry{
		Name:        "get-server",
		Scheme:      "http",
		Host:        "localhost",
		Ports:       []string{"3000:8080"},
		Path:        "/sse",
		Enabled:     true,
		Description: "Test server",
	}
	cfg := loadMCPForTest(t, configPath)
	if err := cfg.CreateEntry(expectedServer); err != nil {
		t.Fatalf("Failed to add server: %v", err)
	}

	server, err := cfg.ReadEntry("get-server")
	if err != nil {
		t.Fatalf("ReadEntry() failed: %v", err)
	}
	if server.Name != expectedServer.Name {
		t.Errorf("Expected name %q, got %q", expectedServer.Name, server.Name)
	}
	if server.GetURL() != expectedServer.GetURL() {
		t.Errorf("Expected URL %q, got %q", expectedServer.GetURL(), server.GetURL())
	}
}

func TestReadEntry_MCPServer_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	cfg := loadMCPForTest(t, configPath)
	if _, err := cfg.ReadEntry("nonexistent-server"); err == nil {
		t.Fatal("Expected error when getting non-existent server, got nil")
	}
}
