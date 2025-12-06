package services

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestMCPConfigService_Load_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "non-existent.yaml")

	service := NewMCPConfigService(configPath)
	cfg, err := service.Load()

	if err != nil {
		t.Fatalf("Load() should not error for non-existent file, got: %v", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	defaultCfg := config.DefaultMCPConfig()
	if cfg.Enabled != defaultCfg.Enabled {
		t.Errorf("Expected Enabled=%v, got %v", defaultCfg.Enabled, cfg.Enabled)
	}
}

func TestMCPConfigService_Load_ValidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	yamlContent := `enabled: true
connection_timeout: 60
discovery_timeout: 45
servers:
  - name: test-server
    url: http://localhost:3000/sse
    enabled: true
    timeout: 30
    description: Test MCP server
    include_tools:
      - tool1
      - tool2
    exclude_tools:
      - tool3
`

	err := os.WriteFile(configPath, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	service := NewMCPConfigService(configPath)
	cfg, err := service.Load()

	if err != nil {
		t.Fatalf("Load() failed: %v", err)
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

	if server.URL != "http://localhost:3000/sse" {
		t.Errorf("Expected server URL 'http://localhost:3000/sse', got %q", server.URL)
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

func TestMCPConfigService_Load_EnvironmentVariableExpansion(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	if err := os.Setenv("TEST_MCP_URL", "http://env-server:8080/sse"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("TEST_MCP_URL"); err != nil {
			t.Logf("Failed to unset environment variable: %v", err)
		}
	}()

	yamlContent := `enabled: true
servers:
  - name: env-server
    url: ${TEST_MCP_URL}
    enabled: true
`

	err := os.WriteFile(configPath, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	service := NewMCPConfigService(configPath)
	cfg, err := service.Load()

	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if len(cfg.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(cfg.Servers))
	}

	server := cfg.Servers[0]
	expectedURL := "http://env-server:8080/sse"
	if server.URL != expectedURL {
		t.Errorf("Expected URL %q (from env var), got %q", expectedURL, server.URL)
	}
}

func TestMCPConfigService_Save(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "subdir", "mcp.yaml")

	service := NewMCPConfigService(configPath)

	cfg := &config.MCPConfig{
		Enabled:           true,
		ConnectionTimeout: 60,
		DiscoveryTimeout:  45,
		Servers: []config.MCPServerEntry{
			{
				Name:        "test-server",
				URL:         "http://localhost:3000/sse",
				Enabled:     true,
				Timeout:     30,
				Description: "Test server",
			},
		},
	}

	err := service.Save(cfg)
	if err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	loadedCfg, err := service.Load()
	if err != nil {
		t.Fatalf("Load() after Save() failed: %v", err)
	}

	if loadedCfg.Enabled != cfg.Enabled {
		t.Errorf("Expected Enabled=%v, got %v", cfg.Enabled, loadedCfg.Enabled)
	}

	if len(loadedCfg.Servers) != len(cfg.Servers) {
		t.Errorf("Expected %d servers, got %d", len(cfg.Servers), len(loadedCfg.Servers))
	}
}

func TestMCPConfigService_AddServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	newServer := config.MCPServerEntry{
		Name:        "new-server",
		URL:         "http://localhost:4000/sse",
		Enabled:     true,
		Description: "New server",
	}

	err := service.AddServer(newServer)
	if err != nil {
		t.Fatalf("AddServer() failed: %v", err)
	}

	cfg, err := service.Load()
	if err != nil {
		t.Fatalf("Load() after AddServer() failed: %v", err)
	}

	if len(cfg.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(cfg.Servers))
	}

	if cfg.Servers[0].Name != newServer.Name {
		t.Errorf("Expected server name %q, got %q", newServer.Name, cfg.Servers[0].Name)
	}
}

func TestMCPConfigService_AddServer_DuplicateName(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	server := config.MCPServerEntry{
		Name:    "duplicate-server",
		URL:     "http://localhost:3000/sse",
		Enabled: true,
	}

	err := service.AddServer(server)
	if err != nil {
		t.Fatalf("First AddServer() failed: %v", err)
	}

	err = service.AddServer(server)
	if err == nil {
		t.Fatal("Expected error when adding duplicate server, got nil")
	}
}

func TestMCPConfigService_UpdateServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	initialServer := config.MCPServerEntry{
		Name:    "update-server",
		URL:     "http://localhost:3000/sse",
		Enabled: true,
	}

	err := service.AddServer(initialServer)
	if err != nil {
		t.Fatalf("AddServer() failed: %v", err)
	}

	updatedServer := config.MCPServerEntry{
		Name:        "update-server",
		URL:         "http://localhost:5000/sse",
		Enabled:     false,
		Description: "Updated description",
	}

	err = service.UpdateServer(updatedServer)
	if err != nil {
		t.Fatalf("UpdateServer() failed: %v", err)
	}

	cfg, err := service.Load()
	if err != nil {
		t.Fatalf("Load() after UpdateServer() failed: %v", err)
	}

	if len(cfg.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(cfg.Servers))
	}

	server := cfg.Servers[0]
	if server.URL != updatedServer.URL {
		t.Errorf("Expected URL %q, got %q", updatedServer.URL, server.URL)
	}

	if server.Enabled != updatedServer.Enabled {
		t.Errorf("Expected Enabled=%v, got %v", updatedServer.Enabled, server.Enabled)
	}

	if server.Description != updatedServer.Description {
		t.Errorf("Expected Description %q, got %q", updatedServer.Description, server.Description)
	}
}

func TestMCPConfigService_UpdateServer_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	server := config.MCPServerEntry{
		Name:    "nonexistent-server",
		URL:     "http://localhost:3000/sse",
		Enabled: true,
	}

	err := service.UpdateServer(server)
	if err == nil {
		t.Fatal("Expected error when updating non-existent server, got nil")
	}
}

func TestMCPConfigService_RemoveServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	server1 := config.MCPServerEntry{Name: "server1", URL: "http://localhost:3000/sse", Enabled: true}
	server2 := config.MCPServerEntry{Name: "server2", URL: "http://localhost:4000/sse", Enabled: true}

	if err := service.AddServer(server1); err != nil {
		t.Fatalf("Failed to add server1: %v", err)
	}
	if err := service.AddServer(server2); err != nil {
		t.Fatalf("Failed to add server2: %v", err)
	}

	err := service.RemoveServer("server1")
	if err != nil {
		t.Fatalf("RemoveServer() failed: %v", err)
	}

	cfg, err := service.Load()
	if err != nil {
		t.Fatalf("Load() after RemoveServer() failed: %v", err)
	}

	if len(cfg.Servers) != 1 {
		t.Fatalf("Expected 1 server after removal, got %d", len(cfg.Servers))
	}

	if cfg.Servers[0].Name != "server2" {
		t.Errorf("Expected remaining server to be 'server2', got %q", cfg.Servers[0].Name)
	}
}

func TestMCPConfigService_RemoveServer_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	err := service.RemoveServer("nonexistent-server")
	if err == nil {
		t.Fatal("Expected error when removing non-existent server, got nil")
	}
}

func TestMCPConfigService_ListServers(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	servers, err := service.ListServers()
	if err != nil {
		t.Fatalf("ListServers() failed: %v", err)
	}

	if len(servers) != 0 {
		t.Errorf("Expected 0 servers initially, got %d", len(servers))
	}

	server1 := config.MCPServerEntry{Name: "server1", URL: "http://localhost:3000/sse", Enabled: true}
	server2 := config.MCPServerEntry{Name: "server2", URL: "http://localhost:4000/sse", Enabled: false}

	if err := service.AddServer(server1); err != nil {
		t.Fatalf("Failed to add server1: %v", err)
	}
	if err := service.AddServer(server2); err != nil {
		t.Fatalf("Failed to add server2: %v", err)
	}

	servers, err = service.ListServers()
	if err != nil {
		t.Fatalf("ListServers() failed: %v", err)
	}

	if len(servers) != 2 {
		t.Fatalf("Expected 2 servers, got %d", len(servers))
	}
}

func TestMCPConfigService_GetServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	expectedServer := config.MCPServerEntry{
		Name:        "get-server",
		URL:         "http://localhost:3000/sse",
		Enabled:     true,
		Description: "Test server",
	}

	if err := service.AddServer(expectedServer); err != nil {
		t.Fatalf("Failed to add server: %v", err)
	}

	server, err := service.GetServer("get-server")
	if err != nil {
		t.Fatalf("GetServer() failed: %v", err)
	}

	if server.Name != expectedServer.Name {
		t.Errorf("Expected name %q, got %q", expectedServer.Name, server.Name)
	}

	if server.URL != expectedServer.URL {
		t.Errorf("Expected URL %q, got %q", expectedServer.URL, server.URL)
	}
}

func TestMCPConfigService_GetServer_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	service := NewMCPConfigService(configPath)

	_, err := service.GetServer("nonexistent-server")
	if err == nil {
		t.Fatal("Expected error when getting non-existent server, got nil")
	}
}
