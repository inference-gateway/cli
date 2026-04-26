package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

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

func TestAddMCPServer(t *testing.T) {
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

	if err := config.AddMCPServer(configPath, newServer); err != nil {
		t.Fatalf("AddMCPServer() failed: %v", err)
	}

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		t.Fatalf("LoadMCP() after AddMCPServer() failed: %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != newServer.Name {
		t.Errorf("Expected server name %q, got %q", newServer.Name, cfg.Servers[0].Name)
	}
}

func TestAddMCPServer_DuplicateName(t *testing.T) {
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

	if err := config.AddMCPServer(configPath, server); err != nil {
		t.Fatalf("First AddMCPServer() failed: %v", err)
	}
	if err := config.AddMCPServer(configPath, server); err == nil {
		t.Fatal("Expected error when adding duplicate server, got nil")
	}
}

func TestUpdateMCPServer(t *testing.T) {
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
	if err := config.AddMCPServer(configPath, initialServer); err != nil {
		t.Fatalf("AddMCPServer() failed: %v", err)
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
	if err := config.UpdateMCPServer(configPath, updatedServer); err != nil {
		t.Fatalf("UpdateMCPServer() failed: %v", err)
	}

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		t.Fatalf("LoadMCP() after UpdateMCPServer() failed: %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(cfg.Servers))
	}

	server := cfg.Servers[0]
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

func TestUpdateMCPServer_NotFound(t *testing.T) {
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
	if err := config.UpdateMCPServer(configPath, server); err == nil {
		t.Fatal("Expected error when updating non-existent server, got nil")
	}
}

func TestRemoveMCPServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	server1 := config.MCPServerEntry{Name: "server1", Scheme: "http", Host: "localhost", Ports: []string{"3000:8080"}, Path: "/sse", Enabled: true}
	server2 := config.MCPServerEntry{Name: "server2", Scheme: "http", Host: "localhost", Ports: []string{"4000:8080"}, Path: "/sse", Enabled: true}

	if err := config.AddMCPServer(configPath, server1); err != nil {
		t.Fatalf("Failed to add server1: %v", err)
	}
	if err := config.AddMCPServer(configPath, server2); err != nil {
		t.Fatalf("Failed to add server2: %v", err)
	}

	if err := config.RemoveMCPServer(configPath, "server1"); err != nil {
		t.Fatalf("RemoveMCPServer() failed: %v", err)
	}

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		t.Fatalf("LoadMCP() after RemoveMCPServer() failed: %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Expected 1 server after removal, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "server2" {
		t.Errorf("Expected remaining server to be 'server2', got %q", cfg.Servers[0].Name)
	}
}

func TestRemoveMCPServer_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	if err := config.RemoveMCPServer(configPath, "nonexistent-server"); err == nil {
		t.Fatal("Expected error when removing non-existent server, got nil")
	}
}

func TestListMCPServers(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	servers, err := config.ListMCPServers(configPath)
	if err != nil {
		t.Fatalf("ListMCPServers() failed: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("Expected 0 servers initially, got %d", len(servers))
	}

	server1 := config.MCPServerEntry{Name: "server1", Scheme: "http", Host: "localhost", Ports: []string{"3000:8080"}, Path: "/sse", Enabled: true}
	server2 := config.MCPServerEntry{Name: "server2", Scheme: "http", Host: "localhost", Ports: []string{"4000:8080"}, Path: "/sse", Enabled: false}

	if err := config.AddMCPServer(configPath, server1); err != nil {
		t.Fatalf("Failed to add server1: %v", err)
	}
	if err := config.AddMCPServer(configPath, server2); err != nil {
		t.Fatalf("Failed to add server2: %v", err)
	}

	servers, err = config.ListMCPServers(configPath)
	if err != nil {
		t.Fatalf("ListMCPServers() failed: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("Expected 2 servers, got %d", len(servers))
	}
}

func TestGetMCPServer(t *testing.T) {
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
	if err := config.AddMCPServer(configPath, expectedServer); err != nil {
		t.Fatalf("Failed to add server: %v", err)
	}

	server, err := config.GetMCPServer(configPath, "get-server")
	if err != nil {
		t.Fatalf("GetMCPServer() failed: %v", err)
	}
	if server.Name != expectedServer.Name {
		t.Errorf("Expected name %q, got %q", expectedServer.Name, server.Name)
	}
	if server.GetURL() != expectedServer.GetURL() {
		t.Errorf("Expected URL %q, got %q", expectedServer.GetURL(), server.GetURL())
	}
}

func TestGetMCPServer_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.yaml")

	if _, err := config.GetMCPServer(configPath, "nonexistent-server"); err == nil {
		t.Fatal("Expected error when getting non-existent server, got nil")
	}
}
