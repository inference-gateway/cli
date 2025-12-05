package services

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	yaml "gopkg.in/yaml.v3"
)

// MCPConfigService manages the mcp.yaml configuration
type MCPConfigService struct {
	configPath string
}

// NewMCPConfigService creates a new MCP config service
func NewMCPConfigService(configPath string) *MCPConfigService {
	return &MCPConfigService{
		configPath: configPath,
	}
}

// Load loads the MCP configuration from the file
func (s *MCPConfigService) Load() (*config.MCPConfig, error) {
	if _, err := os.Stat(s.configPath); os.IsNotExist(err) {
		logger.Info("MCP config file does not exist, returning default", "path", s.configPath)
		return config.DefaultMCPConfig(), nil
	}

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read MCP config: %w", err)
	}

	expandedData := os.ExpandEnv(string(data))

	var mcpConfig config.MCPConfig
	if err := yaml.Unmarshal([]byte(expandedData), &mcpConfig); err != nil {
		return nil, fmt.Errorf("failed to parse MCP config: %w", err)
	}

	return &mcpConfig, nil
}

// Save saves the MCP configuration to the file
func (s *MCPConfigService) Save(cfg *config.MCPConfig) error {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close encoder: %w", err)
	}

	data := buf.Bytes()

	configDir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(s.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write MCP config: %w", err)
	}

	logger.Info("MCP config saved", "path", s.configPath)
	return nil
}

// AddServer adds a new MCP server to the configuration
func (s *MCPConfigService) AddServer(server config.MCPServerEntry) error {
	cfg, err := s.Load()
	if err != nil {
		return err
	}

	for _, existing := range cfg.Servers {
		if existing.Name == server.Name {
			return fmt.Errorf("MCP server with name '%s' already exists", server.Name)
		}
	}

	cfg.Servers = append(cfg.Servers, server)
	return s.Save(cfg)
}

// UpdateServer updates an existing MCP server in the configuration
func (s *MCPConfigService) UpdateServer(server config.MCPServerEntry) error {
	cfg, err := s.Load()
	if err != nil {
		return err
	}

	found := false
	for i, existing := range cfg.Servers {
		if existing.Name == server.Name {
			cfg.Servers[i] = server
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("MCP server with name '%s' not found", server.Name)
	}

	return s.Save(cfg)
}

// RemoveServer removes an MCP server by name
func (s *MCPConfigService) RemoveServer(name string) error {
	cfg, err := s.Load()
	if err != nil {
		return err
	}

	found := false
	newServers := make([]config.MCPServerEntry, 0, len(cfg.Servers))
	for _, server := range cfg.Servers {
		if server.Name != name {
			newServers = append(newServers, server)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("MCP server with name '%s' not found", name)
	}

	cfg.Servers = newServers
	return s.Save(cfg)
}

// ListServers returns all configured MCP servers
func (s *MCPConfigService) ListServers() ([]config.MCPServerEntry, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	return cfg.Servers, nil
}

// GetServer returns a specific MCP server by name
func (s *MCPConfigService) GetServer(name string) (*config.MCPServerEntry, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}

	for _, server := range cfg.Servers {
		if server.Name == name {
			return &server, nil
		}
	}

	return nil, fmt.Errorf("MCP server with name '%s' not found", name)
}

// Merge merges the optional mcp.yaml config with the base config
// The optional config takes precedence for global settings
// Servers from both configs are combined (optional servers can override base servers by name)
func Merge(base *config.MCPConfig, optional *config.MCPConfig) *config.MCPConfig {
	if optional == nil {
		return base
	}

	merged := &config.MCPConfig{
		Enabled:           optional.Enabled || base.Enabled,
		ConnectionTimeout: base.ConnectionTimeout,
		DiscoveryTimeout:  base.DiscoveryTimeout,
		Servers:           make([]config.MCPServerEntry, 0),
	}

	// Optional config overrides global settings if specified
	if optional.ConnectionTimeout > 0 {
		merged.ConnectionTimeout = optional.ConnectionTimeout
	}
	if optional.DiscoveryTimeout > 0 {
		merged.DiscoveryTimeout = optional.DiscoveryTimeout
	}

	// Merge servers: start with base servers
	serverMap := make(map[string]config.MCPServerEntry)
	for _, server := range base.Servers {
		serverMap[server.Name] = server
	}

	// Override/add servers from optional config
	for _, server := range optional.Servers {
		serverMap[server.Name] = server
	}

	// Convert map back to slice
	for _, server := range serverMap {
		merged.Servers = append(merged.Servers, server)
	}

	return merged
}
